package kiss

import (
	"bufio"
	"bytes"
	"errors"
	"io"
)

const (
	// QueueDepth is how many frames to buffer for each TNC port before
	// the oldest frame is dropped.
	QueueDepth = 512
)

const (
	FEND  = 0xC0 // Frame delimiter
	FESC  = 0xDB // Escape character
	TFEND = 0xDC // Transposed FEND
	TFESC = 0xDD // Transposed FESC
)

var (
	ErrInvalidPort = errors.New("invalid port: must be 0-7")
)

// TODO: Add a slog logger that defaults to io.Discard;
// Package function can change the destination.

type TNC struct {
	ports [8]port
}

type port struct {
	id    uint8
	rw    io.ReadWriter
	queue chan []byte
}

func NewTNC(rw io.ReadWriter) *TNC {
	t := &TNC{}
	for i := range t.ports {
		t.ports[i] = port{
			id:    uint8(i),
			rw:    rw,
			queue: make(chan []byte, QueueDepth),
		}
	}

	go t.router(rw)

	return t
}

func (t *TNC) router(rd io.Reader) {
	scanner := bufio.NewScanner(rd)
	scanner.Split(Split)
	for scanner.Scan() {
		// we have a frame in scanner.Bytes()
		frame := scanner.Bytes()
		port := frame[0] >> 4 // will definitely be < 8
		t.enqueue(port, frame[1:])
	}

	// There was an error with Scanner; most likely closed but regardless
	// we cannot recover. Close all the queues so readers report EOF
	for i := range t.ports {
		close(t.ports[i].queue)
	}
}

func (t *TNC) enqueue(port uint8, data []byte) {
	// first check how much space we have left:
	if t.ports[port].free() == 0 {
		// discard one
		<-t.ports[port].queue
	}

	t.ports[port].queue <- data
}

func (t *TNC) Port(n uint8) *port {
	n = min(n, 7)
	return &t.ports[n]
}

func (p *port) Read(data []byte) (n int, err error) {
	frame, ok := <-p.queue // Block until a frame is available.
	if !ok {
		return 0, io.EOF // Channel closed.
	}
	copy(data, frame)
	return len(frame), nil
}

func (p *port) Write(data []byte) (n int, err error) {
	_, err = p.rw.Write(FrameEncode(p.id<<4, data))
	if err != nil {
		n = len(data)
	}
	return
}

func (p *port) free() int {
	return cap(p.queue) - len(p.queue)
}

func FrameEncode(portCmd byte, data []byte) []byte {
	// if we have no escaped bytes, len(data)+3 is spot on
	buf := bytes.NewBuffer(make([]byte, len(data)+3))
	buf.WriteByte(FEND)
	buf.WriteByte(portCmd)
	for i := range len(data) {
		switch data[i] {
		case FEND:
			buf.Write([]byte{FESC, TFEND})
		case FESC:
			buf.Write([]byte{FESC, TFESC})
		default:
			buf.WriteByte(data[i])
		}
	}
	// ensure we hit minimum frame size
	if len(data) <= 14 {
		buf.Write(bytes.Repeat([]byte{0}, 14-len(data)))
	}
	buf.WriteByte(FEND)

	return buf.Bytes()
}

// KissSplit is a bufio.SplitFunc for splitting KISS frames.
func Split(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}

	start := bytes.IndexByte(data, FEND)
	if start == -1 {
		// No frame start found; skip to EOF or wait for more data.
		if atEOF {
			return len(data), nil, nil
		}
		return 0, nil, nil
	}

	end := bytes.IndexByte(data[start+1:], FEND)
	if end == -1 {
		// Frame start found but no end; wait for more data.
		if atEOF {
			return len(data), nil, errors.New("incomplete KISS frame")
		}
		return 0, nil, nil
	}

	// Adjust end to be relative to the original data slice.
	end += start + 1

	// Extract the raw frame data (excluding delimiters).
	rawFrame := data[start+1 : end]

	// Process escape sequences in the frame.
	frame := make([]byte, 0, len(rawFrame))
	i := 0
	for i < len(rawFrame) {
		if rawFrame[i] == FESC {
			if i+1 >= len(rawFrame) {
				// Incomplete escape sequence; wait for more data.
				if atEOF {
					return len(data), nil, errors.New("incomplete escape sequence")
				}
				return 0, nil, nil
			}
			switch rawFrame[i+1] {
			case TFEND:
				frame = append(frame, FEND)
			case TFESC:
				frame = append(frame, FESC)
			default:
				return len(data), nil, errors.New("invalid escape sequence")
			}
			i += 2
		} else {
			frame = append(frame, rawFrame[i])
			i++
		}
	}

	// Return the processed frame as the token.
	return end + 1, frame, nil
}
