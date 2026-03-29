//go:build linux && cgo

package main

/*
#cgo LDFLAGS: -ldl
#include <stddef.h>
#include <stdint.h>
#include <stdlib.h>

typedef int nvrtcResult;
typedef void* nvrtcProgram;

static nvrtcResult codex_nvrtcCreateProgram(
	void* fn,
	nvrtcProgram* prog,
	const char* src,
	const char* name,
	int numHeaders,
	const char** headers,
	const char** includeNames
) {
	return ((nvrtcResult (*)(nvrtcProgram*, const char*, const char*, int, const char**, const char**))fn)(
		prog, src, name, numHeaders, headers, includeNames
	);
}

static nvrtcResult codex_nvrtcCompileProgram(void* fn, nvrtcProgram prog, int numOptions, const char** options) {
	return ((nvrtcResult (*)(nvrtcProgram, int, const char**))fn)(prog, numOptions, options);
}

static nvrtcResult codex_nvrtcDestroyProgram(void* fn, nvrtcProgram* prog) {
	return ((nvrtcResult (*)(nvrtcProgram*))fn)(prog);
}

static nvrtcResult codex_nvrtcGetCUBINSize(void* fn, nvrtcProgram prog, size_t* size) {
	return ((nvrtcResult (*)(nvrtcProgram, size_t*))fn)(prog, size);
}

static nvrtcResult codex_nvrtcGetCUBIN(void* fn, nvrtcProgram prog, char* cubin) {
	return ((nvrtcResult (*)(nvrtcProgram, char*))fn)(prog, cubin);
}

static nvrtcResult codex_nvrtcGetProgramLogSize(void* fn, nvrtcProgram prog, size_t* size) {
	return ((nvrtcResult (*)(nvrtcProgram, size_t*))fn)(prog, size);
}

static nvrtcResult codex_nvrtcGetProgramLog(void* fn, nvrtcProgram prog, char* log) {
	return ((nvrtcResult (*)(nvrtcProgram, char*))fn)(prog, log);
}

static const char* codex_nvrtcGetErrorString(void* fn, nvrtcResult status) {
	return ((const char* (*)(nvrtcResult))fn)(status);
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

type nvrtcProgram C.nvrtcProgram

type nvrtcAPI struct {
	handle             unsafe.Pointer
	createProgram      unsafe.Pointer
	compileProgram     unsafe.Pointer
	destroyProgram     unsafe.Pointer
	getCUBINSize       unsafe.Pointer
	getCUBIN           unsafe.Pointer
	getProgramLogSize  unsafe.Pointer
	getProgramLog      unsafe.Pointer
	getErrorStringProc unsafe.Pointer
}

func loadNVRTC() (*nvrtcAPI, error) {
	if err := prepareCUDALibraryPath(); err != nil {
		return nil, err
	}

	handle, err := openDynamicLibrary(cudaLibraryCandidates([]string{
		"libnvrtc.so.13",
		"libnvrtc.so",
		"libnvrtc.so.12",
		"libnvrtc.so*",
	}))
	if err != nil {
		return nil, fmt.Errorf("load NVRTC library: %w", err)
	}

	api := &nvrtcAPI{handle: handle}
	for _, symbol := range []struct {
		name string
		dst  *unsafe.Pointer
	}{
		{name: "nvrtcCreateProgram", dst: &api.createProgram},
		{name: "nvrtcCompileProgram", dst: &api.compileProgram},
		{name: "nvrtcDestroyProgram", dst: &api.destroyProgram},
		{name: "nvrtcGetCUBINSize", dst: &api.getCUBINSize},
		{name: "nvrtcGetCUBIN", dst: &api.getCUBIN},
		{name: "nvrtcGetProgramLogSize", dst: &api.getProgramLogSize},
		{name: "nvrtcGetProgramLog", dst: &api.getProgramLog},
		{name: "nvrtcGetErrorString", dst: &api.getErrorStringProc},
	} {
		ptr, lookupErr := lookupDynamicSymbol(handle, symbol.name)
		if lookupErr != nil {
			closeDynamicLibrary(handle)
			return nil, lookupErr
		}
		*symbol.dst = ptr
	}

	return api, nil
}

func (api *nvrtcAPI) compileCUDA(source, name string, options []string) ([]byte, string, error) {
	src := C.CString(source)
	defer C.free(unsafe.Pointer(src))
	progName := C.CString(name)
	defer C.free(unsafe.Pointer(progName))

	var program C.nvrtcProgram
	if err := api.check(uint32(C.codex_nvrtcCreateProgram(
		api.createProgram,
		&program,
		src,
		progName,
		0,
		nil,
		nil,
	))); err != nil {
		return nil, "", err
	}
	defer api.destroy(nvrtcProgram(program))

	optionPointers := make([]*C.char, 0, len(options))
	for _, option := range options {
		ptr := C.CString(option)
		optionPointers = append(optionPointers, ptr)
	}
	defer func() {
		for _, ptr := range optionPointers {
			C.free(unsafe.Pointer(ptr))
		}
	}()

	var optionsPtr **C.char
	if len(optionPointers) > 0 {
		optionsPtr = (**C.char)(unsafe.Pointer(&optionPointers[0]))
	}

	compileStatus := uint32(C.codex_nvrtcCompileProgram(
		api.compileProgram,
		program,
		C.int(len(optionPointers)),
		optionsPtr,
	))

	logOutput := api.programLog(nvrtcProgram(program))
	if err := api.check(compileStatus); err != nil {
		return nil, logOutput, fmt.Errorf("nvrtc compile failed: %w", err)
	}

	cubin, err := api.programCUBIN(nvrtcProgram(program))
	if err != nil {
		return nil, logOutput, err
	}
	return cubin, logOutput, nil
}

func (api *nvrtcAPI) programLog(program nvrtcProgram) string {
	var size C.size_t
	if err := api.check(uint32(C.codex_nvrtcGetProgramLogSize(api.getProgramLogSize, C.nvrtcProgram(program), &size))); err != nil {
		return err.Error()
	}
	if size <= 1 {
		return ""
	}

	buffer := make([]byte, int(size))
	if err := api.check(uint32(C.codex_nvrtcGetProgramLog(
		api.getProgramLog,
		C.nvrtcProgram(program),
		(*C.char)(unsafe.Pointer(&buffer[0])),
	))); err != nil {
		return err.Error()
	}
	return trimCString(buffer)
}

func (api *nvrtcAPI) programCUBIN(program nvrtcProgram) ([]byte, error) {
	var size C.size_t
	if err := api.check(uint32(C.codex_nvrtcGetCUBINSize(api.getCUBINSize, C.nvrtcProgram(program), &size))); err != nil {
		return nil, err
	}

	buffer := make([]byte, int(size))
	if err := api.check(uint32(C.codex_nvrtcGetCUBIN(
		api.getCUBIN,
		C.nvrtcProgram(program),
		(*C.char)(unsafe.Pointer(&buffer[0])),
	))); err != nil {
		return nil, err
	}
	return buffer, nil
}

func (api *nvrtcAPI) destroy(program nvrtcProgram) {
	cProgram := C.nvrtcProgram(program)
	_ = api.check(uint32(C.codex_nvrtcDestroyProgram(api.destroyProgram, &cProgram)))
}

func (api *nvrtcAPI) check(status uint32) error {
	if status == 0 {
		return nil
	}

	message := fmt.Sprintf("NVRTC error code %d", status)
	if api.getErrorStringProc != nil {
		if ptr := C.codex_nvrtcGetErrorString(api.getErrorStringProc, C.nvrtcResult(status)); ptr != nil {
			message = C.GoString(ptr)
		}
	}
	return fmt.Errorf("%s", message)
}
