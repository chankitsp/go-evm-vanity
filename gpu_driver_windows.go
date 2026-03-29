//go:build windows

package main

import (
	"fmt"
	"syscall"
	"unsafe"
)

const (
	cuDeviceAttributeComputeCapabilityMajor = 75
	cuDeviceAttributeComputeCapabilityMinor = 76
)

type cudaDriver struct {
	dll                *syscall.LazyDLL
	initProc           *syscall.LazyProc
	deviceGetProc      *syscall.LazyProc
	deviceGetAttrProc  *syscall.LazyProc
	ctxCreateProc      *syscall.LazyProc
	ctxDestroyProc     *syscall.LazyProc
	ctxSyncProc        *syscall.LazyProc
	moduleLoadDataProc *syscall.LazyProc
	moduleUnloadProc   *syscall.LazyProc
	moduleGetFuncProc  *syscall.LazyProc
	memAllocProc       *syscall.LazyProc
	memFreeProc        *syscall.LazyProc
	memcpyHtoDProc     *syscall.LazyProc
	memcpyDtoHProc     *syscall.LazyProc
	launchKernelProc   *syscall.LazyProc
	getErrorStringProc *syscall.LazyProc
}

func loadCUDADriver() (*cudaDriver, error) {
	dll := syscall.NewLazyDLL(`C:\Windows\System32\nvcuda.dll`)
	if err := dll.Load(); err != nil {
		return nil, fmt.Errorf("load CUDA driver DLL: %w", err)
	}

	driver := &cudaDriver{
		dll:                dll,
		initProc:           dll.NewProc("cuInit"),
		deviceGetProc:      dll.NewProc("cuDeviceGet"),
		deviceGetAttrProc:  dll.NewProc("cuDeviceGetAttribute"),
		ctxCreateProc:      dll.NewProc("cuCtxCreate_v2"),
		ctxDestroyProc:     dll.NewProc("cuCtxDestroy_v2"),
		ctxSyncProc:        dll.NewProc("cuCtxSynchronize"),
		moduleLoadDataProc: dll.NewProc("cuModuleLoadData"),
		moduleUnloadProc:   dll.NewProc("cuModuleUnload"),
		moduleGetFuncProc:  dll.NewProc("cuModuleGetFunction"),
		memAllocProc:       dll.NewProc("cuMemAlloc_v2"),
		memFreeProc:        dll.NewProc("cuMemFree_v2"),
		memcpyHtoDProc:     dll.NewProc("cuMemcpyHtoD_v2"),
		memcpyDtoHProc:     dll.NewProc("cuMemcpyDtoH_v2"),
		launchKernelProc:   dll.NewProc("cuLaunchKernel"),
		getErrorStringProc: dll.NewProc("cuGetErrorString"),
	}

	if err := driver.check(callStatus(driver.initProc.Call(0))); err != nil {
		return nil, err
	}
	return driver, nil
}

func (d *cudaDriver) createContext() (uintptr, int, int, error) {
	var device int32
	if err := d.check(callStatus(d.deviceGetProc.Call(uintptr(unsafe.Pointer(&device)), 0))); err != nil {
		return 0, 0, 0, err
	}

	major, err := d.deviceAttribute(device, cuDeviceAttributeComputeCapabilityMajor)
	if err != nil {
		return 0, 0, 0, err
	}
	minor, err := d.deviceAttribute(device, cuDeviceAttributeComputeCapabilityMinor)
	if err != nil {
		return 0, 0, 0, err
	}

	var context uintptr
	if err := d.check(callStatus(d.ctxCreateProc.Call(
		uintptr(unsafe.Pointer(&context)),
		0,
		uintptr(device),
	))); err != nil {
		return 0, 0, 0, err
	}

	return context, major, minor, nil
}

func (d *cudaDriver) destroyContext(context uintptr) {
	if context != 0 {
		_, _, _ = d.ctxDestroyProc.Call(context)
	}
}

func (d *cudaDriver) deviceAttribute(device int32, attribute int32) (int, error) {
	var value int32
	if err := d.check(callStatus(d.deviceGetAttrProc.Call(
		uintptr(unsafe.Pointer(&value)),
		uintptr(attribute),
		uintptr(device),
	))); err != nil {
		return 0, err
	}
	return int(value), nil
}

func (d *cudaDriver) loadModule(image []byte) (uintptr, error) {
	if len(image) == 0 {
		return 0, fmt.Errorf("empty CUDA image")
	}

	var module uintptr
	if err := d.check(callStatus(d.moduleLoadDataProc.Call(
		uintptr(unsafe.Pointer(&module)),
		uintptr(unsafe.Pointer(&image[0])),
	))); err != nil {
		return 0, err
	}
	return module, nil
}

func (d *cudaDriver) unloadModule(module uintptr) {
	if module != 0 {
		_, _, _ = d.moduleUnloadProc.Call(module)
	}
}

func (d *cudaDriver) getFunction(module uintptr, name string) (uintptr, error) {
	namePtr, err := syscall.BytePtrFromString(name)
	if err != nil {
		return 0, err
	}

	var function uintptr
	if err := d.check(callStatus(d.moduleGetFuncProc.Call(
		uintptr(unsafe.Pointer(&function)),
		module,
		uintptr(unsafe.Pointer(namePtr)),
	))); err != nil {
		return 0, err
	}
	return function, nil
}

func (d *cudaDriver) alloc(size uintptr) (uint64, error) {
	var ptr uint64
	if err := d.check(callStatus(d.memAllocProc.Call(
		uintptr(unsafe.Pointer(&ptr)),
		size,
	))); err != nil {
		return 0, err
	}
	return ptr, nil
}

func (d *cudaDriver) free(ptr uint64) {
	if ptr != 0 {
		_, _, _ = d.memFreeProc.Call(uintptr(ptr))
	}
}

func (d *cudaDriver) memcpyHtoD(dst uint64, src []byte) error {
	if len(src) == 0 {
		return nil
	}
	return d.check(callStatus(d.memcpyHtoDProc.Call(
		uintptr(dst),
		uintptr(unsafe.Pointer(&src[0])),
		uintptr(len(src)),
	)))
}

func (d *cudaDriver) memcpyDtoH(dst []byte, src uint64) error {
	if len(dst) == 0 {
		return nil
	}
	return d.check(callStatus(d.memcpyDtoHProc.Call(
		uintptr(unsafe.Pointer(&dst[0])),
		uintptr(src),
		uintptr(len(dst)),
	)))
}

func (d *cudaDriver) sync() error {
	return d.check(callStatus(d.ctxSyncProc.Call()))
}

func (d *cudaDriver) launch(function uintptr, gridX, blockX uint32, params []unsafe.Pointer) error {
	var paramsPtr uintptr
	if len(params) > 0 {
		paramsPtr = uintptr(unsafe.Pointer(&params[0]))
	}

	return d.check(callStatus(d.launchKernelProc.Call(
		function,
		uintptr(gridX), 1, 1,
		uintptr(blockX), 1, 1,
		0,
		0,
		paramsPtr,
		0,
	)))
}

func (d *cudaDriver) launchPubkey(function uintptr, gridX, blockX uint32, dScalars uint64, count int32, dPubKeys uint64, dStatuses uint64) error {
	params := []unsafe.Pointer{
		unsafe.Pointer(&dScalars),
		unsafe.Pointer(&count),
		unsafe.Pointer(&dPubKeys),
		unsafe.Pointer(&dStatuses),
	}
	return d.launch(function, gridX, blockX, params)
}

func (d *cudaDriver) check(status uint32) error {
	if status == 0 {
		return nil
	}

	message := fmt.Sprintf("CUDA driver error code %d", status)
	if d.getErrorStringProc != nil {
		var strPtr uintptr
		_, _, _ = d.getErrorStringProc.Call(uintptr(status), uintptr(unsafe.Pointer(&strPtr)))
		if strPtr != 0 {
			message = readCString(strPtr)
		}
	}
	return fmt.Errorf("%s", message)
}
