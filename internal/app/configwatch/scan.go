package configwatch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"

	"github.com/hay-kot/hive-desktop/internal/app/configstate"
)

type scanResult struct {
	topology Topology
	revision configstate.Revision
}

type topologySynchronizer func(Topology)

type sourceScanner func(context.Context, Authority, topologySynchronizer) (scanResult, error)

func scanAuthority(ctx context.Context, authority Authority, synchronize topologySynchronizer) (scanResult, error) {
	for attempt := range 2 {
		if err := ctx.Err(); err != nil {
			return scanResult{}, err
		}
		topology, err := authority.Topology(ctx)
		if err != nil {
			return scanResult{}, fmt.Errorf("authority topology: %w", err)
		}
		for _, dir := range topology.Directories {
			info, err := os.Stat(dir)
			if err != nil {
				return scanResult{}, fmt.Errorf("authority directory %q: %w", dir, err)
			}
			if !info.IsDir() {
				return scanResult{}, fmt.Errorf("authority directory %q is not a directory", dir)
			}
		}
		synchronize(topology)

		files := append([]AuthorityFile(nil), topology.Files...)
		sort.Slice(files, func(i, j int) bool { return files[i].Key < files[j].Key })
		parts := make([]configstate.RevisionPart, 0, len(files))
		missing := false
		for _, file := range files {
			revision, state, err := fileRevision(ctx, file.Path)
			if err != nil {
				return scanResult{}, fmt.Errorf("authority file %q: %w", file.Key, err)
			}
			missing = missing || state == configstate.Missing
			parts = append(parts, configstate.RevisionPart{Key: file.Key, State: state, Revision: revision})
		}
		if missing && attempt == 0 {
			continue
		}
		return scanResult{topology: topology, revision: configstate.AggregateRevision(parts)}, nil
	}
	panic("unreachable")
}

func fileRevision(ctx context.Context, path string) (configstate.Revision, configstate.CandidateState, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", configstate.Missing, nil
		}
		return "", "", err
	}
	if info.IsDir() {
		return "", "", fmt.Errorf("path is a directory")
	}

	file, err := openFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", configstate.Missing, nil
		}
		return "", "", err
	}

	results := make(chan readResult, 1)
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		defer close(results)
		for {
			chunk := make([]byte, 32*1024)
			n, err := file.Read(chunk)
			if n > 0 {
				result := readResult{data: chunk[:n], err: err}
				select {
				case results <- result:
				case <-ctx.Done():
					return
				}
			}
			if err != nil {
				if n == 0 {
					select {
					case results <- readResult{err: err}:
					case <-ctx.Done():
					}
				}
				return
			}
		}
	}()
	defer func() {
		_ = file.Close()
		<-readDone
	}()

	h := sha256.New()
	writeRevisionFrame(h, "bytes")
	_, _ = io.WriteString(h, strconv.FormatInt(info.Size(), 10)+":")
	var read int64
	for {
		select {
		case <-ctx.Done():
			return "", "", ctx.Err()
		case result, ok := <-results:
			if err := ctx.Err(); err != nil {
				return "", "", err
			}
			if !ok {
				return "", "", io.ErrUnexpectedEOF
			}
			if len(result.data) > 0 {
				_, _ = h.Write(result.data)
				read += int64(len(result.data))
			}
			if errors.Is(result.err, io.EOF) {
				if read != info.Size() {
					return "", "", fmt.Errorf("file changed during read")
				}
				_, _ = h.Write([]byte{0})
				return configstate.Revision(hex.EncodeToString(h.Sum(nil))), configstate.Valid, nil
			}
			if result.err != nil {
				return "", "", result.err
			}
		}
	}
}

type readResult struct {
	data []byte
	err  error
}

type readFile interface {
	io.Reader
	io.Closer
}

var openFile = func(path string) (readFile, error) { return os.Open(path) }

func writeRevisionFrame(w io.Writer, value string) {
	_, _ = io.WriteString(w, strconv.Itoa(len(value))+":"+value)
	_, _ = w.Write([]byte{0})
}
