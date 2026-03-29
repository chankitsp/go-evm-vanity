//go:build windows

package main

import (
	"fmt"
	"path/filepath"
	"syscall"
	"unsafe"
)

type nvrtcProgram uintptr

type nvrtcAPI struct {
	dll                *syscall.LazyDLL
	createProgram      *syscall.LazyProc
	compileProgram     *syscall.LazyProc
	destroyProgram     *syscall.LazyProc
	getCUBINSize       *syscall.LazyProc
	getCUBIN           *syscall.LazyProc
	getProgramLogSize  *syscall.LazyProc
	getProgramLog      *syscall.LazyProc
	getErrorStringProc *syscall.LazyProc
}

func loadNVRTC() (*nvrtcAPI, error) {
	if err := prepareCUDALibraryPath(); err != nil {
		return nil, err
	}

	binX64Dir, err := cudaBinX64Dir()
	if err != nil {
		return nil, err
	}

	dllPath := filepath.Join(binX64Dir, "nvrtc64_130_0.dll")
	dll := syscall.NewLazyDLL(dllPath)
	if err := dll.Load(); err != nil {
		return nil, fmt.Errorf("load NVRTC DLL: %w", err)
	}

	return &nvrtcAPI{
		dll:                dll,
		createProgram:      dll.NewProc("nvrtcCreateProgram"),
		compileProgram:     dll.NewProc("nvrtcCompileProgram"),
		destroyProgram:     dll.NewProc("nvrtcDestroyProgram"),
		getCUBINSize:       dll.NewProc("nvrtcGetCUBINSize"),
		getCUBIN:           dll.NewProc("nvrtcGetCUBIN"),
		getProgramLogSize:  dll.NewProc("nvrtcGetProgramLogSize"),
		getProgramLog:      dll.NewProc("nvrtcGetProgramLog"),
		getErrorStringProc: dll.NewProc("nvrtcGetErrorString"),
	}, nil
}

func (api *nvrtcAPI) compileCUDA(source, name string, options []string) ([]byte, string, error) {
	srcPtr, err := syscall.BytePtrFromString(source)
	if err != nil {
		return nil, "", err
	}
	namePtr, err := syscall.BytePtrFromString(name)
	if err != nil {
		return nil, "", err
	}

	var program nvrtcProgram
	if err := api.check(callStatus(api.createProgram.Call(
		uintptr(unsafe.Pointer(&program)),
		uintptr(unsafe.Pointer(srcPtr)),
		uintptr(unsafe.Pointer(namePtr)),
		0,
		0,
		0,
	))); err != nil {
		return nil, "", err
	}
	defer api.destroy(program)

	optionPointers := make([]*byte, 0, len(options))
	for _, option := range options {
		ptr, err := syscall.BytePtrFromString(option)
		if err != nil {
			return nil, "", err
		}
		optionPointers = append(optionPointers, ptr)
	}

	var optionsPtr uintptr
	if len(optionPointers) > 0 {
		optionsPtr = uintptr(unsafe.Pointer(&optionPointers[0]))
	}

	compileStatus, _, _ := api.compileProgram.Call(
		uintptr(program),
		uintptr(len(optionPointers)),
		optionsPtr,
	)

	logOutput := api.programLog(program)
	if err := api.check(uint32(compileStatus)); err != nil {
		return nil, logOutput, fmt.Errorf("nvrtc compile failed: %w", err)
	}

	cubin, err := api.programCUBIN(program)
	if err != nil {
		return nil, logOutput, err
	}
	return cubin, logOutput, nil
}

func (api *nvrtcAPI) programLog(program nvrtcProgram) string {
	var size uintptr
	if err := api.check(callStatus(api.getProgramLogSize.Call(uintptr(program), uintptr(unsafe.Pointer(&size))))); err != nil {
		return err.Error()
	}
	if size <= 1 {
		return ""
	}

	buffer := make([]byte, size)
	if err := api.check(callStatus(api.getProgramLog.Call(uintptr(program), uintptr(unsafe.Pointer(&buffer[0]))))); err != nil {
		return err.Error()
	}
	return trimCString(buffer)
}

func (api *nvrtcAPI) programCUBIN(program nvrtcProgram) ([]byte, error) {
	var size uintptr
	if err := api.check(callStatus(api.getCUBINSize.Call(uintptr(program), uintptr(unsafe.Pointer(&size))))); err != nil {
		return nil, err
	}

	buffer := make([]byte, size)
	if err := api.check(callStatus(api.getCUBIN.Call(uintptr(program), uintptr(unsafe.Pointer(&buffer[0]))))); err != nil {
		return nil, err
	}
	return buffer, nil
}

func (api *nvrtcAPI) destroy(program nvrtcProgram) {
	_ = api.check(callStatus(api.destroyProgram.Call(uintptr(unsafe.Pointer(&program)))))
}

func (api *nvrtcAPI) check(status uint32) error {
	if status == 0 {
		return nil
	}

	message := fmt.Sprintf("NVRTC error code %d", status)
	if api.getErrorStringProc != nil {
		if ptr, _, _ := api.getErrorStringProc.Call(uintptr(status)); ptr != 0 {
			message = readCString(ptr)
		}
	}
	return fmt.Errorf("%s", message)
}

func callStatus(r1 uintptr, _ uintptr, _ error) uint32 {
	return uint32(r1)
}
