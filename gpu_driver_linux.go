//go:build linux && cgo

package main

/*
#cgo LDFLAGS: -ldl
#include <stdint.h>
#include <stddef.h>
#include <stdlib.h>

typedef int CUresult;
typedef int CUdevice;
typedef uintptr_t CUcontext;
typedef uintptr_t CUmodule;
typedef uintptr_t CUfunction;
typedef uint64_t CUdeviceptr;
typedef int CUdevice_attribute;

static CUresult codex_cuInit(void* fn, unsigned int flags) {
	return ((CUresult (*)(unsigned int))fn)(flags);
}

static CUresult codex_cuDeviceGet(void* fn, CUdevice* device, int ordinal) {
	return ((CUresult (*)(CUdevice*, int))fn)(device, ordinal);
}

static CUresult codex_cuDeviceGetAttribute(void* fn, int* value, CUdevice_attribute attribute, CUdevice device) {
	return ((CUresult (*)(int*, CUdevice_attribute, CUdevice))fn)(value, attribute, device);
}

static CUresult codex_cuCtxCreate(void* fn, CUcontext* context, unsigned int flags, CUdevice device) {
	return ((CUresult (*)(CUcontext*, unsigned int, CUdevice))fn)(context, flags, device);
}

static CUresult codex_cuCtxDestroy(void* fn, CUcontext context) {
	return ((CUresult (*)(CUcontext))fn)(context);
}

static CUresult codex_cuCtxSynchronize(void* fn) {
	return ((CUresult (*)())fn)();
}

static CUresult codex_cuModuleLoadData(void* fn, CUmodule* module, const void* image) {
	return ((CUresult (*)(CUmodule*, const void*))fn)(module, image);
}

static CUresult codex_cuModuleUnload(void* fn, CUmodule module) {
	return ((CUresult (*)(CUmodule))fn)(module);
}

static CUresult codex_cuModuleGetFunction(void* fn, CUfunction* function, CUmodule module, const char* name) {
	return ((CUresult (*)(CUfunction*, CUmodule, const char*))fn)(function, module, name);
}

static CUresult codex_cuMemAlloc(void* fn, CUdeviceptr* ptr, size_t size) {
	return ((CUresult (*)(CUdeviceptr*, size_t))fn)(ptr, size);
}

static CUresult codex_cuMemFree(void* fn, CUdeviceptr ptr) {
	return ((CUresult (*)(CUdeviceptr))fn)(ptr);
}

static CUresult codex_cuMemcpyHtoD(void* fn, CUdeviceptr dst, const void* src, size_t size) {
	return ((CUresult (*)(CUdeviceptr, const void*, size_t))fn)(dst, src, size);
}

static CUresult codex_cuMemcpyDtoH(void* fn, void* dst, CUdeviceptr src, size_t size) {
	return ((CUresult (*)(void*, CUdeviceptr, size_t))fn)(dst, src, size);
}

static CUresult codex_cuLaunchKeccakKernel(
	void* fn,
	CUfunction function,
	unsigned int gridX,
	unsigned int blockX,
	uint64_t dPubKeys,
	int32_t count,
	uint64_t dSuffixBytes,
	int32_t suffixLenBytes,
	int32_t oddSuffix,
	unsigned char leadingHalf,
	uint64_t dFoundIndex
) {
	CUdeviceptr pubkeys = (CUdeviceptr)dPubKeys;
	CUdeviceptr suffixBytes = (CUdeviceptr)dSuffixBytes;
	CUdeviceptr foundIndex = (CUdeviceptr)dFoundIndex;
	void* kernelParams[] = {
		&pubkeys,
		&count,
		&suffixBytes,
		&suffixLenBytes,
		&oddSuffix,
		&leadingHalf,
		&foundIndex
	};
	return ((CUresult (*)(CUfunction, unsigned int, unsigned int, unsigned int, unsigned int, unsigned int, unsigned int, unsigned int, void*, void**, void**))fn)(
		function, gridX, 1, 1, blockX, 1, 1, 0, NULL, kernelParams, NULL
	);
}

static CUresult codex_cuGetErrorString(void* fn, CUresult status, const char** out) {
	return ((CUresult (*)(CUresult, const char**))fn)(status, out);
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

const (
	cuDeviceAttributeComputeCapabilityMajor = 75
	cuDeviceAttributeComputeCapabilityMinor = 76
)

type cudaDriver struct {
	handle             unsafe.Pointer
	initProc           unsafe.Pointer
	deviceGetProc      unsafe.Pointer
	deviceGetAttrProc  unsafe.Pointer
	ctxCreateProc      unsafe.Pointer
	ctxDestroyProc     unsafe.Pointer
	ctxSyncProc        unsafe.Pointer
	moduleLoadDataProc unsafe.Pointer
	moduleUnloadProc   unsafe.Pointer
	moduleGetFuncProc  unsafe.Pointer
	memAllocProc       unsafe.Pointer
	memFreeProc        unsafe.Pointer
	memcpyHtoDProc     unsafe.Pointer
	memcpyDtoHProc     unsafe.Pointer
	launchKernelProc   unsafe.Pointer
	getErrorStringProc unsafe.Pointer
}

func loadCUDADriver() (*cudaDriver, error) {
	handle, err := openDynamicLibrary(cudaLibraryCandidates([]string{"libcuda.so.1", "libcuda.so"}))
	if err != nil {
		return nil, fmt.Errorf("load CUDA driver library: %w", err)
	}

	driver := &cudaDriver{handle: handle}
	for _, symbol := range []struct {
		name string
		dst  *unsafe.Pointer
	}{
		{name: "cuInit", dst: &driver.initProc},
		{name: "cuDeviceGet", dst: &driver.deviceGetProc},
		{name: "cuDeviceGetAttribute", dst: &driver.deviceGetAttrProc},
		{name: "cuCtxCreate_v2", dst: &driver.ctxCreateProc},
		{name: "cuCtxDestroy_v2", dst: &driver.ctxDestroyProc},
		{name: "cuCtxSynchronize", dst: &driver.ctxSyncProc},
		{name: "cuModuleLoadData", dst: &driver.moduleLoadDataProc},
		{name: "cuModuleUnload", dst: &driver.moduleUnloadProc},
		{name: "cuModuleGetFunction", dst: &driver.moduleGetFuncProc},
		{name: "cuMemAlloc_v2", dst: &driver.memAllocProc},
		{name: "cuMemFree_v2", dst: &driver.memFreeProc},
		{name: "cuMemcpyHtoD_v2", dst: &driver.memcpyHtoDProc},
		{name: "cuMemcpyDtoH_v2", dst: &driver.memcpyDtoHProc},
		{name: "cuLaunchKernel", dst: &driver.launchKernelProc},
		{name: "cuGetErrorString", dst: &driver.getErrorStringProc},
	} {
		ptr, lookupErr := lookupDynamicSymbol(handle, symbol.name)
		if lookupErr != nil {
			closeDynamicLibrary(handle)
			return nil, lookupErr
		}
		*symbol.dst = ptr
	}

	if err := driver.check(uint32(C.codex_cuInit(driver.initProc, 0))); err != nil {
		closeDynamicLibrary(handle)
		return nil, err
	}
	return driver, nil
}

func (d *cudaDriver) createContext() (uintptr, int, int, error) {
	var device C.CUdevice
	if err := d.check(uint32(C.codex_cuDeviceGet(d.deviceGetProc, &device, 0))); err != nil {
		return 0, 0, 0, err
	}

	major, err := d.deviceAttribute(int32(device), cuDeviceAttributeComputeCapabilityMajor)
	if err != nil {
		return 0, 0, 0, err
	}
	minor, err := d.deviceAttribute(int32(device), cuDeviceAttributeComputeCapabilityMinor)
	if err != nil {
		return 0, 0, 0, err
	}

	var context C.CUcontext
	if err := d.check(uint32(C.codex_cuCtxCreate(d.ctxCreateProc, &context, 0, device))); err != nil {
		return 0, 0, 0, err
	}

	return uintptr(context), major, minor, nil
}

func (d *cudaDriver) destroyContext(context uintptr) {
	if context != 0 {
		_ = d.check(uint32(C.codex_cuCtxDestroy(d.ctxDestroyProc, C.CUcontext(context))))
	}
}

func (d *cudaDriver) deviceAttribute(device int32, attribute int32) (int, error) {
	var value C.int
	if err := d.check(uint32(C.codex_cuDeviceGetAttribute(
		d.deviceGetAttrProc,
		&value,
		C.CUdevice_attribute(attribute),
		C.CUdevice(device),
	))); err != nil {
		return 0, err
	}
	return int(value), nil
}

func (d *cudaDriver) loadModule(image []byte) (uintptr, error) {
	if len(image) == 0 {
		return 0, fmt.Errorf("empty CUDA image")
	}

	var module C.CUmodule
	if err := d.check(uint32(C.codex_cuModuleLoadData(
		d.moduleLoadDataProc,
		&module,
		unsafe.Pointer(&image[0]),
	))); err != nil {
		return 0, err
	}
	return uintptr(module), nil
}

func (d *cudaDriver) unloadModule(module uintptr) {
	if module != 0 {
		_ = d.check(uint32(C.codex_cuModuleUnload(d.moduleUnloadProc, C.CUmodule(module))))
	}
}

func (d *cudaDriver) getFunction(module uintptr, name string) (uintptr, error) {
	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))

	var function C.CUfunction
	if err := d.check(uint32(C.codex_cuModuleGetFunction(
		d.moduleGetFuncProc,
		&function,
		C.CUmodule(module),
		cName,
	))); err != nil {
		return 0, err
	}
	return uintptr(function), nil
}

func (d *cudaDriver) alloc(size uintptr) (uint64, error) {
	var ptr C.CUdeviceptr
	if err := d.check(uint32(C.codex_cuMemAlloc(d.memAllocProc, &ptr, C.size_t(size)))); err != nil {
		return 0, err
	}
	return uint64(ptr), nil
}

func (d *cudaDriver) free(ptr uint64) {
	if ptr != 0 {
		_ = d.check(uint32(C.codex_cuMemFree(d.memFreeProc, C.CUdeviceptr(ptr))))
	}
}

func (d *cudaDriver) memcpyHtoD(dst uint64, src []byte) error {
	if len(src) == 0 {
		return nil
	}
	return d.check(uint32(C.codex_cuMemcpyHtoD(
		d.memcpyHtoDProc,
		C.CUdeviceptr(dst),
		unsafe.Pointer(&src[0]),
		C.size_t(len(src)),
	)))
}

func (d *cudaDriver) memcpyDtoH(dst []byte, src uint64) error {
	if len(dst) == 0 {
		return nil
	}
	return d.check(uint32(C.codex_cuMemcpyDtoH(
		d.memcpyDtoHProc,
		unsafe.Pointer(&dst[0]),
		C.CUdeviceptr(src),
		C.size_t(len(dst)),
	)))
}

func (d *cudaDriver) sync() error {
	return d.check(uint32(C.codex_cuCtxSynchronize(d.ctxSyncProc)))
}

func (d *cudaDriver) launch(function uintptr, gridX, blockX uint32, params []unsafe.Pointer) error {
	if len(params) != 7 {
		return fmt.Errorf("unexpected CUDA kernel parameter count: %d", len(params))
	}

	dPubKeys := *(*C.uint64_t)(params[0])
	count32 := *(*C.int32_t)(params[1])
	dSuffixBytes := *(*C.uint64_t)(params[2])
	suffixLenBytes := *(*C.int32_t)(params[3])
	oddSuffix := *(*C.int32_t)(params[4])
	leadingHalf := *(*C.uchar)(params[5])
	dFoundIndex := *(*C.uint64_t)(params[6])

	return d.check(uint32(C.codex_cuLaunchKeccakKernel(
		d.launchKernelProc,
		C.CUfunction(function),
		C.uint(gridX),
		C.uint(blockX),
		dPubKeys,
		count32,
		dSuffixBytes,
		suffixLenBytes,
		oddSuffix,
		leadingHalf,
		dFoundIndex,
	)))
}

func (d *cudaDriver) check(status uint32) error {
	if status == 0 {
		return nil
	}

	message := fmt.Sprintf("CUDA driver error code %d", status)
	if d.getErrorStringProc != nil {
		var str *C.char
		_ = C.codex_cuGetErrorString(d.getErrorStringProc, C.CUresult(status), (**C.char)(unsafe.Pointer(&str)))
		if str != nil {
			message = C.GoString(str)
		}
	}
	return fmt.Errorf("%s", message)
}
