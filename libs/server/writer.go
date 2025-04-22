package server

import (
	"bufio"
	"errors"
	"fmt"
	"lug/util"
	"net"
	"net/http"
)

type Writer struct {
	ResponseWriter http.ResponseWriter
	ReadWriter     *bufio.ReadWriter
	Conn           net.Conn
	TimedOut       bool
	hijacked       bool
	written        bool
	length         int
	statusCode     int
}

func (w *Writer) Reset(res http.ResponseWriter) {
	w.ResponseWriter = res
	w.ReadWriter = nil
	w.Conn = nil
	w.TimedOut = false
	w.hijacked = false
	w.written = false
	w.length = 0
	w.statusCode = http.StatusOK
}

func (w *Writer) Length() int {
	return w.length
}

func (w *Writer) Write(body []byte) (int, error) {
	// Check if the response is already hijacked or timed out.
	if err := w.Hijacked(); err != nil {
		return 0, err
	}

	// If the header hasn't been written yet, write the default status code.
	if !w.written {
		if err := w.writeHeader(w.statusCode); err != nil {
			return 0, err
		}
	}

	length, err := w.ResponseWriter.Write(body)
	if err != nil {
		return 0, fmt.Errorf("error writing to response writer: %w", err)
	}

	w.length += length
	return length, nil
}

func (w *Writer) WriteHeader(statusCode int) error {
	if w.written {
		return errors.New("superfluous response.WriteHeader")
	}
	if err := w.Hijacked(); err != nil {
		return err
	}
	return w.writeHeader(statusCode)
}

func (w *Writer) writeHeader(statusCode int) error {
	switch {
	case statusCode == 0:
		statusCode = http.StatusOK
	case !util.CheckStatusCode(statusCode):
		return fmt.Errorf("invalid status code: %d", statusCode)
	}
	w.written = true
	w.length = 0
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
	return nil
}

func (w *Writer) Flush() error {
	if err := w.Hijacked(); err != nil {
		return err
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
	return nil
}

func (w *Writer) Hijack() error {

	if err := w.Hijacked(); err != nil {
		return err
	}

	if w.written {
		return errors.New("connection already written")
	}

	hiJacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return errors.New("connection doesn't support hijacking")
	}

	conn, bufrw, err := hiJacker.Hijack()
	if err != nil {
		return err
	}

	w.Conn = conn
	w.ReadWriter = bufrw
	w.hijacked = true

	return nil
}

func (w *Writer) ReadHijack(maxLength ...int) ([]byte, error) {

	if !w.hijacked || w.ReadWriter == nil {
		return nil, errors.New("connection not hijacked")
	}

	length := 1024
	if len(maxLength) > 0 {
		length = maxLength[0]
	}

	buf := make([]byte, length)
	n, err := w.ReadWriter.Read(buf)
	if err != nil {
		return nil, err
	}
	return buf[:n], nil
}

func (w *Writer) WriteHijack(body []byte) (int, error) {

	if !w.hijacked || w.ReadWriter == nil {
		return 0, errors.New("connection not hijacked")
	}

	n, err := w.ReadWriter.Write(body)
	if err != nil {
		return 0, err
	}
	if err := w.ReadWriter.Flush(); err != nil {
		return 0, err
	}

	w.length += n
	return n, nil
}

func (w *Writer) CloseHijack() {
	if w.Conn != nil {
		w.Conn.Close()
		w.Conn = nil
		w.ReadWriter = nil
		// w.hijacked = false
	}
}

func (w *Writer) Hijacked() error {
	switch {
	case w.hijacked:
		return errors.New("response already hijacked")
	case w.TimedOut:
		return errors.New("response processing timeout")
	default:
		return nil
	}
}

func (w *Writer) Written() error {
	if w.written {
		return errors.New("response already written")
	}
	return w.Hijacked()
}
