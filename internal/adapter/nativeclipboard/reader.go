package nativeclipboard

import "context"

type Reader struct{}

func (Reader) ReadImage(ctx context.Context) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return readImage()
}
