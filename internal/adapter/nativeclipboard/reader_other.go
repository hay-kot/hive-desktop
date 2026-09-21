//go:build !darwin || !cgo || server

package nativeclipboard

func readImage() ([]byte, error) { return nil, nil }
