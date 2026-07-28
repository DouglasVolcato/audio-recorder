package devices

import (
	"encoding/binary"
	"fmt"
	"os"
	"time"
	"unsafe"

	"github.com/moutend/go-wca/pkg/wca"
)

const (
	sampleRate    = 48000
	channels      = 1
	bitsPerSample = 16

	blockAlign = channels * bitsPerSample / 8

	qpcUnitsPerSecond = 10_000_000

	microphoneGain = 3.0
	computerGain   = 0.8
)

func newPCMFormat() *wca.WAVEFORMATEX {
	return &wca.WAVEFORMATEX{
		WFormatTag:      wca.WAVE_FORMAT_PCM,
		NChannels:       channels,
		NSamplesPerSec:  sampleRate,
		NAvgBytesPerSec: sampleRate * blockAlign,
		NBlockAlign:     blockAlign,
		WBitsPerSample:  bitsPerSample,
		CbSize:          0,
	}
}

func openCapture(
	device *wca.IMMDevice,
	loopback bool,
) (
	audioClient *wca.IAudioClient,
	captureClient *wca.IAudioCaptureClient,
	err error,
) {
	if device == nil {
		return nil, nil, fmt.Errorf("dispositivo de áudio é nil")
	}

	err = device.Activate(
		wca.IID_IAudioClient,
		wca.CLSCTX_ALL,
		nil,
		&audioClient,
	)
	if err != nil {
		return nil, nil, fmt.Errorf(
			"ativar IAudioClient: %w",
			err,
		)
	}

	flags := uint32(
		wca.AUDCLNT_STREAMFLAGS_AUTOCONVERTPCM |
			wca.AUDCLNT_STREAMFLAGS_SRC_DEFAULT_QUALITY,
	)

	if loopback {
		flags |= wca.AUDCLNT_STREAMFLAGS_LOOPBACK
	}

	format := newPCMFormat()

	err = audioClient.Initialize(
		wca.AUDCLNT_SHAREMODE_SHARED,
		flags,
		wca.REFERENCE_TIME(100*10000), // 100 ms
		0,
		format,
		nil,
	)
	if err != nil {
		audioClient.Release()

		return nil, nil, fmt.Errorf(
			"inicializar captura: %w",
			err,
		)
	}

	err = audioClient.GetService(
		wca.IID_IAudioCaptureClient,
		&captureClient,
	)
	if err != nil {
		audioClient.Release()

		return nil, nil, fmt.Errorf(
			"obter IAudioCaptureClient: %w",
			err,
		)
	}

	return audioClient, captureClient, nil
}

func Record(
	inputDevice *wca.IMMDevice,
	outputDevice *wca.IMMDevice,
	filename string,
	stop <-chan struct{},
) error {
	if filename == "" {
		filename = "gravacao.wav"
	}

	inputAudio, inputCapture, err := openCapture(
		inputDevice,
		false,
	)
	if err != nil {
		return fmt.Errorf("abrir microfone: %w", err)
	}

	defer inputCapture.Release()
	defer inputAudio.Release()

	outputAudio, outputCapture, err := openCapture(
		outputDevice,
		true,
	)
	if err != nil {
		return fmt.Errorf("abrir loopback: %w", err)
	}

	defer outputCapture.Release()
	defer outputAudio.Release()

	// Começa primeiro o loopback e depois o microfone.
	// A diferença entre os dois Starts normalmente é mínima.
	if err := outputAudio.Start(); err != nil {
		return fmt.Errorf(
			"iniciar captura do áudio do computador: %w",
			err,
		)
	}

	defer outputAudio.Stop()

	if err := inputAudio.Start(); err != nil {
		return fmt.Errorf(
			"iniciar captura do microfone: %w",
			err,
		)
	}

	defer inputAudio.Stop()

	var inputPackets []capturePacket
	var outputPackets []capturePacket

	recording := true
	for recording {
		select {
		case <-stop:
			recording = false
		default:
		}
		if !recording {
			break
		}

		inputData, err := readAvailable(inputCapture)
		if err != nil {
			return fmt.Errorf(
				"ler microfone: %w",
				err,
			)
		}

		if len(inputData) > 0 {
			inputPackets = append(inputPackets, inputData...)
		}

		outputData, err := readAvailable(outputCapture)
		if err != nil {
			return fmt.Errorf(
				"ler áudio do computador: %w",
				err,
			)
		}

		if len(outputData) > 0 {
			outputPackets = append(outputPackets, outputData...)
		}
		time.Sleep(5 * time.Millisecond)
	}

	inputData, err := readAvailable(inputCapture)
	if err == nil && len(inputData) > 0 {
		inputPackets = append(inputPackets, inputData...)
	}

	outputData, err := readAvailable(outputCapture)
	if err == nil && len(outputData) > 0 {
		outputPackets = append(outputPackets, outputData...)
	}

	inputPCM, outputPCM := alignCapturedPCM(
		inputPackets,
		outputPackets,
	)

	mixedPCM := mixPCM16(
		inputPCM,
		outputPCM,
		microphoneGain,
		computerGain,
	)

	if err := writePCM16WAV(
		filename,
		mixedPCM,
	); err != nil {
		return fmt.Errorf(
			"salvar arquivo WAV: %w",
			err,
		)
	}

	return nil
}

type capturePacket struct {
	data        []byte
	qpcPosition uint64
}

func readAvailable(
	capture *wca.IAudioCaptureClient,
) ([]capturePacket, error) {
	if capture == nil {
		return nil, fmt.Errorf("capture client é nil")
	}

	var result []capturePacket

	for {
		var packetFrames uint32

		if err := capture.GetNextPacketSize(
			&packetFrames,
		); err != nil {
			return nil, fmt.Errorf(
				"obter tamanho do próximo pacote: %w",
				err,
			)
		}

		if packetFrames == 0 {
			return result, nil
		}

		var data *byte
		var frames uint32
		var flags uint32
		var devicePosition uint64
		var qpcPosition uint64

		if err := capture.GetBuffer(
			&data,
			&frames,
			&flags,
			&devicePosition,
			&qpcPosition,
		); err != nil {
			return nil, fmt.Errorf(
				"obter buffer de áudio: %w",
				err,
			)
		}

		size := int(frames) * blockAlign
		chunk := make([]byte, size)

		if flags&wca.AUDCLNT_BUFFERFLAGS_SILENT == 0 &&
			data != nil &&
			size > 0 {
			source := unsafe.Slice(data, size)
			copy(chunk, source)
		}

		if err := capture.ReleaseBuffer(frames); err != nil {
			return nil, fmt.Errorf(
				"liberar buffer de áudio: %w",
				err,
			)
		}

		result = append(result, capturePacket{
			data:        chunk,
			qpcPosition: qpcPosition,
		})
	}
}

func alignCapturedPCM(
	inputPackets []capturePacket,
	outputPackets []capturePacket,
) ([]byte, []byte) {
	startQPC := firstCaptureTimestamp(inputPackets)
	outputStartQPC := firstCaptureTimestamp(outputPackets)

	if startQPC == 0 ||
		(outputStartQPC != 0 && outputStartQPC < startQPC) {
		startQPC = outputStartQPC
	}

	return buildPCMTimeline(inputPackets, startQPC),
		buildPCMTimeline(outputPackets, startQPC)
}

func firstCaptureTimestamp(packets []capturePacket) uint64 {
	for _, packet := range packets {
		if packet.qpcPosition != 0 {
			return packet.qpcPosition
		}
	}

	return 0
}

func buildPCMTimeline(
	packets []capturePacket,
	startQPC uint64,
) []byte {
	var timeline []byte
	var sequentialOffset int

	for _, packet := range packets {
		offset := sequentialOffset
		if startQPC != 0 && packet.qpcPosition >= startQPC {
			frames := (packet.qpcPosition-startQPC)*sampleRate +
				qpcUnitsPerSecond/2
			offset = int(frames/qpcUnitsPerSecond) * blockAlign
		}

		end := offset + len(packet.data)
		if end > len(timeline) {
			timeline = append(timeline, make([]byte, end-len(timeline))...)
		}

		copy(timeline[offset:end], packet.data)
		sequentialOffset = end
	}

	return timeline
}

func fitLength(data []byte, size int) []byte {
	result := make([]byte, size)
	copy(result, data)

	return result
}

func mixPCM16(
	input []byte,
	output []byte,
	inputGain float64,
	outputGain float64,
) []byte {
	size := len(input)

	if len(output) > size {
		size = len(output)
	}

	size -= size % 2

	mixed := make([]byte, size)

	for i := 0; i < size; i += 2 {
		var inputSample int16
		if i+2 <= len(input) {
			inputSample = int16(
				binary.LittleEndian.Uint16(
					input[i : i+2],
				),
			)
		}

		var outputSample int16
		if i+2 <= len(output) {
			outputSample = int16(
				binary.LittleEndian.Uint16(
					output[i : i+2],
				),
			)
		}

		value :=
			float64(inputSample)*inputGain +
				float64(outputSample)*outputGain

		value = clampPCM16(value)

		binary.LittleEndian.PutUint16(
			mixed[i:i+2],
			uint16(int16(value)),
		)
	}

	return mixed
}

func clampPCM16(value float64) float64 {
	if value > 32767 {
		return 32767
	}

	if value < -32768 {
		return -32768
	}

	return value
}

func writePCM16WAV(
	filename string,
	pcm []byte,
) error {
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf(
			"criar arquivo %q: %w",
			filename,
			err,
		)
	}

	defer file.Close()

	dataSize := uint32(len(pcm))
	byteRate := uint32(sampleRate * blockAlign)

	if _, err := file.Write([]byte("RIFF")); err != nil {
		return err
	}

	if err := binary.Write(
		file,
		binary.LittleEndian,
		uint32(36)+dataSize,
	); err != nil {
		return err
	}

	if _, err := file.Write([]byte("WAVE")); err != nil {
		return err
	}

	if _, err := file.Write([]byte("fmt ")); err != nil {
		return err
	}

	if err := binary.Write(
		file,
		binary.LittleEndian,
		uint32(16),
	); err != nil {
		return err
	}

	if err := binary.Write(
		file,
		binary.LittleEndian,
		uint16(1),
	); err != nil {
		return err
	}

	if err := binary.Write(
		file,
		binary.LittleEndian,
		uint16(channels),
	); err != nil {
		return err
	}

	if err := binary.Write(
		file,
		binary.LittleEndian,
		uint32(sampleRate),
	); err != nil {
		return err
	}

	if err := binary.Write(
		file,
		binary.LittleEndian,
		byteRate,
	); err != nil {
		return err
	}

	if err := binary.Write(
		file,
		binary.LittleEndian,
		uint16(blockAlign),
	); err != nil {
		return err
	}

	if err := binary.Write(
		file,
		binary.LittleEndian,
		uint16(bitsPerSample),
	); err != nil {
		return err
	}

	if _, err := file.Write([]byte("data")); err != nil {
		return err
	}

	if err := binary.Write(
		file,
		binary.LittleEndian,
		dataSize,
	); err != nil {
		return err
	}

	if _, err := file.Write(pcm); err != nil {
		return err
	}

	return file.Sync()
}
