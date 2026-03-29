//go:build linux && cgo

package main

/*
#cgo LDFLAGS: -ldl
#include <dlfcn.h>
#include <stdlib.h>

static void* codex_dlopen(const char* path) {
	return dlopen(path, RTLD_NOW | RTLD_LOCAL);
}

static const char* codex_dlerror() {
	return dlerror();
}

static void* codex_dlsym(void* handle, const char* name) {
	return dlsym(handle, name);
}

static int codex_dlclose(void* handle) {
	return dlclose(handle);
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

func trimCString(buffer []byte) string {
	end := 0
	for end < len(buffer) && buffer[end] != 0 {
		end++
	}
	return string(buffer[:end])
}

func readCString(ptr uintptr) string {
	if ptr == 0 {
		return ""
	}
	return C.GoString((*C.char)(unsafe.Pointer(ptr)))
}

func openDynamicLibrary(candidates []string) (unsafe.Pointer, error) {
	var lastErr string

	for _, candidate := range candidates {
		path := C.CString(candidate)
		handle := C.codex_dlopen(path)
		C.free(unsafe.Pointer(path))
		if handle != nil {
			return handle, nil
		}
		if err := lastDLError(); err != "" {
			lastErr = err
		}
	}

	if lastErr == "" {
		lastErr = "unknown dlopen error"
	}
	return nil, fmt.Errorf("%s", lastErr)
}

func lookupDynamicSymbol(handle unsafe.Pointer, name string) (unsafe.Pointer, error) {
	symbol := C.CString(name)
	ptr := C.codex_dlsym(handle, symbol)
	C.free(unsafe.Pointer(symbol))
	if ptr != nil {
		return ptr, nil
	}

	if err := lastDLError(); err != "" {
		return nil, fmt.Errorf("%s", err)
	}
	return nil, fmt.Errorf("symbol %q not found", name)
}

func closeDynamicLibrary(handle unsafe.Pointer) {
	if handle != nil {
		_ = C.codex_dlclose(handle)
	}
}

func lastDLError() string {
	if ptr := C.codex_dlerror(); ptr != nil {
		return C.GoString(ptr)
	}
	return ""
}
