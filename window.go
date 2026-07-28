package main

import (
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"github.com/douglasvolcato/audio-recorder/devices"
	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

const (
	startButtonID = 1
	stopButtonID  = 2
)

var (
	recorderUI   *recorderWindow
	windowProc   = windows.NewCallback(recorderWindowProc)
	windowClass  = utf16Ptr("AudioRecorderWindow")
	windowTitle  = utf16Ptr("Gravador de áudio")
	buttonClass  = utf16Ptr("BUTTON")
	staticClass  = utf16Ptr("STATIC")
	startText    = utf16Ptr("Iniciar gravação")
	stopText     = utf16Ptr("Parar gravação")
	initialState = "Pronto para gravar."
)

type recorderWindow struct {
	hWnd        win.HWND
	status      win.HWND
	startButton win.HWND
	stopButton  win.HWND

	mu            sync.Mutex
	recording     bool
	stopRecording chan struct{}
}

func runRecorderWindow() error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	instance := win.GetModuleHandle(nil)
	windowClassDefinition := win.WNDCLASSEX{
		CbSize:        uint32(unsafe.Sizeof(win.WNDCLASSEX{})),
		LpfnWndProc:   windowProc,
		HInstance:     instance,
		HCursor:       win.LoadCursor(0, win.MAKEINTRESOURCE(win.IDC_ARROW)),
		HbrBackground: win.HBRUSH(win.COLOR_WINDOW + 1),
		LpszClassName: windowClass,
	}

	if win.RegisterClassEx(&windowClassDefinition) == 0 {
		return fmt.Errorf("registrar janela")
	}

	ui := &recorderWindow{}
	recorderUI = ui

	ui.hWnd = win.CreateWindowEx(
		0,
		windowClass,
		windowTitle,
		win.WS_CAPTION|win.WS_SYSMENU|win.WS_MINIMIZEBOX,
		win.CW_USEDEFAULT,
		win.CW_USEDEFAULT,
		360,
		155,
		0,
		0,
		instance,
		nil,
	)
	if ui.hWnd == 0 {
		return fmt.Errorf("criar janela")
	}

	ui.status = win.CreateWindowEx(
		0,
		staticClass,
		utf16Ptr(initialState),
		win.WS_CHILD|win.WS_VISIBLE,
		16,
		18,
		320,
		24,
		ui.hWnd,
		0,
		instance,
		nil,
	)

	ui.startButton = win.CreateWindowEx(
		0,
		buttonClass,
		startText,
		win.WS_CHILD|win.WS_VISIBLE|win.WS_TABSTOP|win.BS_PUSHBUTTON,
		16,
		62,
		150,
		32,
		ui.hWnd,
		win.HMENU(startButtonID),
		instance,
		nil,
	)

	ui.stopButton = win.CreateWindowEx(
		0,
		buttonClass,
		stopText,
		win.WS_CHILD|win.WS_VISIBLE|win.WS_TABSTOP|win.BS_PUSHBUTTON,
		178,
		62,
		150,
		32,
		ui.hWnd,
		win.HMENU(stopButtonID),
		instance,
		nil,
	)
	win.EnableWindow(ui.stopButton, false)

	win.ShowWindow(ui.hWnd, win.SW_SHOW)

	var message win.MSG
	for win.GetMessage(&message, 0, 0, 0) != 0 {
		win.TranslateMessage(&message)
		win.DispatchMessage(&message)
	}

	return nil
}

func recorderWindowProc(
	hWnd win.HWND,
	message uint32,
	wParam uintptr,
	lParam uintptr,
) uintptr {
	if recorderUI == nil {
		return win.DefWindowProc(hWnd, message, wParam, lParam)
	}

	switch message {
	case win.WM_COMMAND:
		switch uint16(wParam) {
		case startButtonID:
			recorderUI.start()
		case stopButtonID:
			recorderUI.stop()
		}
		return 0
	case win.WM_CLOSE:
		if recorderUI.stop() {
			recorderUI.setStatus("Parando gravação. Feche novamente quando terminar.")
			return 0
		}
		win.DestroyWindow(hWnd)
		return 0
	case win.WM_DESTROY:
		win.PostQuitMessage(0)
		return 0
	}

	return win.DefWindowProc(hWnd, message, wParam, lParam)
}

func (ui *recorderWindow) start() {
	ui.mu.Lock()
	if ui.recording {
		ui.mu.Unlock()
		return
	}

	ui.recording = true
	ui.stopRecording = make(chan struct{})
	stop := ui.stopRecording
	ui.mu.Unlock()

	win.EnableWindow(ui.startButton, false)
	win.EnableWindow(ui.stopButton, true)
	ui.setStatus("Abrindo dispositivos...")

	go ui.record(stop)
}

func (ui *recorderWindow) stop() bool {
	ui.mu.Lock()
	defer ui.mu.Unlock()

	if !ui.recording {
		return false
	}

	if ui.stopRecording != nil {
		close(ui.stopRecording)
		ui.stopRecording = nil
		ui.setStatus("Parando gravação...")
	}

	return true
}

func (ui *recorderWindow) record(stop <-chan struct{}) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	inputDevice, outputDevice, err, cleanup := devices.GetDevices()
	if err == nil {
		defer cleanup()
		ui.setStatus("Gravando...")
		err = devices.Record(inputDevice, outputDevice, "gravacao.wav", stop)
	}

	ui.mu.Lock()
	ui.recording = false
	ui.mu.Unlock()

	if err != nil {
		ui.setStatus("Erro: " + err.Error())
	} else {
		ui.setStatus("Arquivo criado: gravacao.wav")
	}

	win.EnableWindow(ui.startButton, true)
	win.EnableWindow(ui.stopButton, false)
}

func (ui *recorderWindow) setStatus(text string) {
	win.SendMessage(
		ui.status,
		win.WM_SETTEXT,
		0,
		uintptr(unsafe.Pointer(utf16Ptr(text))),
	)
}

func utf16Ptr(value string) *uint16 {
	result, _ := windows.UTF16PtrFromString(value)
	return result
}
