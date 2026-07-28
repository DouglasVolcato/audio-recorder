package devices

import (
	"fmt"

	"github.com/go-ole/go-ole"
	"github.com/moutend/go-wca/pkg/wca"
)

func GetDevices() (*wca.IMMDevice, *wca.IMMDevice, error, func()) {
	if err := ole.CoInitializeEx(
		0,
		ole.COINIT_APARTMENTTHREADED,
	); err != nil {
		return nil, nil, fmt.Errorf("erro inicializando COM: %w", err), func() {}
	}

	var enumerator *wca.IMMDeviceEnumerator

	if err := wca.CoCreateInstance(
		wca.CLSID_MMDeviceEnumerator,
		0,
		wca.CLSCTX_ALL,
		wca.IID_IMMDeviceEnumerator,
		&enumerator,
	); err != nil {
		return nil, nil, fmt.Errorf("erro criando enumerador de áudio: %w", err), func() {
			defer ole.CoUninitialize()
		}
	}

	var inputDevice *wca.IMMDevice

	if err := enumerator.GetDefaultAudioEndpoint(
		wca.ECapture,
		wca.EConsole,
		&inputDevice,
	); err != nil {
		return nil, nil, fmt.Errorf("erro encontrando entrada padrão: %w", err), func() {
			defer ole.CoUninitialize()
			defer enumerator.Release()
		}
	}

	var outputDevice *wca.IMMDevice

	if err := enumerator.GetDefaultAudioEndpoint(
		wca.ERender,
		wca.EConsole,
		&outputDevice,
	); err != nil {
		return nil, nil, fmt.Errorf("erro encontrando saída padrão: %w", err), func() {
			defer ole.CoUninitialize()
			defer enumerator.Release()
			defer inputDevice.Release()
		}
	}

	return inputDevice, outputDevice, nil, func() {
		defer ole.CoUninitialize()
		defer enumerator.Release()
		defer inputDevice.Release()
		defer outputDevice.Release()
	}
}

func GetDeviceName(device *wca.IMMDevice) (string, error) {
	var propertyStore *wca.IPropertyStore

	if err := device.OpenPropertyStore(
		wca.STGM_READ,
		&propertyStore,
	); err != nil {
		return "", fmt.Errorf("abrir propriedades: %w", err)
	}

	defer propertyStore.Release()

	var value wca.PROPVARIANT

	if err := propertyStore.GetValue(
		&wca.PKEY_Device_FriendlyName,
		&value,
	); err != nil {
		return "", fmt.Errorf("ler nome do dispositivo: %w", err)
	}

	return value.String(), nil
}

func OpenInput(
	device *wca.IMMDevice,
) (
	audioClient *wca.IAudioClient,
	captureClient *wca.IAudioCaptureClient,
	format *wca.WAVEFORMATEX,
	err error,
) {
	err = device.Activate(
		wca.IID_IAudioClient,
		wca.CLSCTX_ALL,
		nil,
		&audioClient,
	)
	if err != nil {
		return nil, nil, nil, fmt.Errorf(
			"ativar IAudioClient: %w",
			err,
		)
	}

	err = audioClient.GetMixFormat(&format)
	if err != nil {
		audioClient.Release()

		return nil, nil, nil, fmt.Errorf(
			"obter formato: %w",
			err,
		)
	}

	var defaultPeriod wca.REFERENCE_TIME
	var minimumPeriod wca.REFERENCE_TIME

	err = audioClient.GetDevicePeriod(
		&defaultPeriod,
		&minimumPeriod,
	)
	if err != nil {
		audioClient.Release()

		return nil, nil, nil, fmt.Errorf(
			"obter período: %w",
			err,
		)
	}

	err = audioClient.Initialize(
		wca.AUDCLNT_SHAREMODE_SHARED,
		0,
		defaultPeriod,
		0,
		format,
		nil,
	)
	if err != nil {
		audioClient.Release()

		return nil, nil, nil, fmt.Errorf(
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

		return nil, nil, nil, fmt.Errorf(
			"obter IAudioCaptureClient: %w",
			err,
		)
	}

	return audioClient, captureClient, format, nil
}

func OpenOutputLoopback(
	device *wca.IMMDevice,
) (
	audioClient *wca.IAudioClient,
	captureClient *wca.IAudioCaptureClient,
	format *wca.WAVEFORMATEX,
	err error,
) {
	err = device.Activate(
		wca.IID_IAudioClient,
		wca.CLSCTX_ALL,
		nil,
		&audioClient,
	)
	if err != nil {
		return nil, nil, nil, fmt.Errorf(
			"ativar IAudioClient: %w",
			err,
		)
	}

	err = audioClient.GetMixFormat(&format)
	if err != nil {
		audioClient.Release()

		return nil, nil, nil, fmt.Errorf(
			"obter formato: %w",
			err,
		)
	}

	err = audioClient.Initialize(
		wca.AUDCLNT_SHAREMODE_SHARED,
		wca.AUDCLNT_STREAMFLAGS_LOOPBACK,
		wca.REFERENCE_TIME(100*10000),
		0,
		format,
		nil,
	)
	if err != nil {
		audioClient.Release()

		return nil, nil, nil, fmt.Errorf(
			"inicializar loopback: %w",
			err,
		)
	}

	err = audioClient.GetService(
		wca.IID_IAudioCaptureClient,
		&captureClient,
	)
	if err != nil {
		audioClient.Release()

		return nil, nil, nil, fmt.Errorf(
			"obter captura loopback: %w",
			err,
		)
	}

	return audioClient, captureClient, format, nil
}
