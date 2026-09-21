//go:build darwin && cgo && !server

package nativeclipboard

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit -framework ImageIO
#include <stdlib.h>
#include "reader.h"
*/
import "C"

import "errors"

func readImage() ([]byte, error) {
	result := C.hiveClipboardImage()
	if result.data != nil {
		defer C.free(result.data)
	}
	if result.status < 0 {
		return nil, errors.New("clipboard image could not be read within image limits")
	}
	if result.status == 0 {
		return nil, nil
	}
	return C.GoBytes(result.data, result.length), nil
}
