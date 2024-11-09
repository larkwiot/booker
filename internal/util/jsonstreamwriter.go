package util

import (
	"fmt"
	"os"
	"strings"
	"sync"
)

type JsonStreamWriterItem struct {
	Key  string
	Data []byte
}

type JsonStreamWriter struct {
	Filepath       string
	Input          chan *JsonStreamWriterItem
	waiter         sync.WaitGroup
	fh             *os.File
	lock           sync.Mutex
	isInitialized  bool
	batchThreshold int
}

func NewJsonStreamWriter(filePath string) (*JsonStreamWriter, error) {
	fh, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0666)
	if err != nil {
		return nil, err
	}
	stream := &JsonStreamWriter{
		Filepath:       filePath,
		Input:          make(chan *JsonStreamWriterItem, 10000),
		waiter:         sync.WaitGroup{},
		fh:             fh,
		lock:           sync.Mutex{},
		isInitialized:  false,
		batchThreshold: 10,
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

func (stream *JsonStreamWriter) writer() {
	for {
		inQueue := len(stream.Input)
		if inQueue >= stream.batchThreshold {
			items := make([]*JsonStreamWriterItem, inQueue)
			for i := range inQueue {
				items[i] = <-stream.Input
			}
			err := stream.WriteBatch(items)
			if err != nil {
				panic(err)
			}
		}
		select {
		case item, isOpen := <-stream.Input:
			if !isOpen {
				stream.waiter.Done()
				return
			}
			if item == nil {
				continue
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

func (stream *JsonStreamWriter) WriteItem(key string, data []byte) error {
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

func (stream *JsonStreamWriter) WriteBatch(items []*JsonStreamWriterItem) error {
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

func (stream *JsonStreamWriter) Close() error {
	close(stream.Input)

	stream.waiter.Wait()

	stream.lock.Lock()
	defer stream.lock.Unlock()

	_, err := stream.fh.WriteString("}")
	if err != nil {
		return err
	}
	err = stream.fh.Sync()
	if err != nil {
		return err
	}

	return stream.fh.Close()
}
