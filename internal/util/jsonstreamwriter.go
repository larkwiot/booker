package util

import (
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
)

type JsonStreamWriterItem struct {
	Key  string
	Data []byte
}

type JsonStreamWriter[I any] struct {
	Filepath       string
	Input          chan JsonStreamWriterItem
	waiter         sync.WaitGroup
	fh             *os.File
	lock           sync.Mutex
	isInitialized  bool
	batchThreshold int
	convert        func(I) (JsonStreamWriterItem, error)
}

func NewJsonStreamWriter[I any](filePath string, convert func(I) (JsonStreamWriterItem, error)) (*JsonStreamWriter[I], error) {
	fh, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0666)
	if err != nil {
		return nil, err
	}
	stream := &JsonStreamWriter[I]{
		Filepath:       filePath,
		Input:          make(chan JsonStreamWriterItem, 10000),
		waiter:         sync.WaitGroup{},
		fh:             fh,
		lock:           sync.Mutex{},
		isInitialized:  false,
		batchThreshold: 10,
		convert:        convert,
	}
	_, err = stream.fh.WriteString("{")
	if err != nil {
		return nil, err
	}
	err = stream.fh.Sync()
	if err != nil {
		return nil, err
	}

	stream.waiter.Add(1)
	go stream.writer()

	return stream, nil
}

func (stream *JsonStreamWriter[I]) writer() {
	for {
		select {
		case item, isOpen := <-stream.Input:
			if !isOpen {
				stream.waiter.Done()
				return
			}
			err := stream.WriteItem(item.Key, item.Data)
			if err != nil {
				panic(err)
			}
		}
	}
}

func formatBuffer(key string, data []byte, initialized bool) string {
	if initialized {
		return fmt.Sprintf(",\"%s\": %s", strings.ReplaceAll(key, "\"", ""), string(data))
	}
	return fmt.Sprintf("\"%s\": %s", strings.ReplaceAll(key, "\"", ""), string(data))
}

func (stream *JsonStreamWriter[I]) WriteItem(key string, data []byte) error {
	stream.lock.Lock()
	defer stream.lock.Unlock()

	s := formatBuffer(key, data, stream.isInitialized)
	stream.isInitialized = true

	_, err := stream.fh.WriteString(s)
	if err != nil {
		return err
	}

	return stream.fh.Sync()
}

func (stream *JsonStreamWriter[I]) WriteBatch(items []*JsonStreamWriterItem) error {
	stream.lock.Lock()
	defer stream.lock.Unlock()

	for _, item := range items {
		s := formatBuffer(item.Key, item.Data, stream.isInitialized)
		stream.isInitialized = true
		_, err := stream.fh.WriteString(s)
		if err != nil {
			return err
		}
	}

	return stream.fh.Sync()
}

func (stream *JsonStreamWriter[I]) WriteObject(obj I) {
	item, err := stream.convert(obj)
	if err != nil {
		log.Printf("warning: could not write item to json stream because conversion failed: %s\n", err.Error())
	}
	stream.Input <- item
}

func (stream *JsonStreamWriter[I]) Close() {
	if stream.Input == nil {
		return
	}

	close(stream.Input)

	stream.waiter.Wait()

	stream.lock.Lock()
	defer stream.lock.Unlock()

	_, err := stream.fh.WriteString("}")
	if err != nil {
		log.Printf("error: failed to write closing bracket: %s\n", err.Error())
		return
	}
	err = stream.fh.Sync()
	if err != nil {
		log.Printf("error: failed to sync, bracket might not be committed to file: %s\n", err.Error())
	}

	err = stream.fh.Close()
	if err != nil {
		log.Printf("error: failed to close file handle: %s\n", err.Error())
	}
}
