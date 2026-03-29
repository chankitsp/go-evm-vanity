//go:build windows

package main

import "unsafe"

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

	buffer := make([]byte, 0, 128)
	for {
		b := *(*byte)(unsafe.Pointer(ptr))
		if b == 0 {
			break
		}
		buffer = append(buffer, b)
		ptr++
	}
	return string(buffer)
}
