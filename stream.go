package smux

import (
	"log"
	"sync"

	"github.com/pkg/errors"
)

// Stream implements io.ReadWriteCloser
type Stream struct {
	id             uint32
	chNotifyReader chan struct{}
	sess           *Session
	frameSize      uint32
	die            chan struct{}
	mu             sync.Mutex
	buffer         []byte
}

func newStream(id uint32, frameSize uint32, chNotifyReader chan struct{}, sess *Session) *Stream {
	s := new(Stream)
	s.id = id
	s.chNotifyReader = chNotifyReader
	s.frameSize = frameSize
	s.sess = sess
	s.die = make(chan struct{})
	f := newFrame(cmdSYN, s.id)
	bts, _ := f.MarshalBinary()
	sess.lw.Write(bts)
	return s
}

// Read implements io.ReadWriteCloser
func (s *Stream) Read(b []byte) (n int, err error) {
	if len(s.buffer) > 0 {
		n = copy(b, s.buffer)
		s.buffer = s.buffer[n:]
		return n, nil
	}

	for {
		f := s.sess.read(s.id)
		if f != nil {
			log.Println(f)
			switch f.cmd {
			case cmdPSH:
				n = copy(b, f.data)
				if len(f.data) > n {
					s.buffer = make([]byte, len(f.data)-n)
					copy(s.buffer, f.data[n:])
				}
				log.Println("push", f)
				return n, nil
			}
		}
	}

	return 0, nil
}

// Write implements io.ReadWriteCloser
func (s *Stream) Write(b []byte) (n int, err error) {
	frames := s.split(b, cmdPSH, s.id)
	log.Println("frames:", len(frames))
	if len(frames) == 0 {
		return 0, errors.New("cannot split frame")
	}
	for k := range frames {
		bts, _ := frames[k].MarshalBinary()
		s.sess.lw.Write(bts)
	}

	return 0, nil
}

// Close implements io.ReadWriteCloser
func (s *Stream) Close() error {
	select {
	case <-s.die:
		return errors.New("broken pipe")
	default:
		close(s.die)
	}
	return nil
}

func (s *Stream) split(bts []byte, cmd byte, sid uint32) (frames []Frame) {
	log.Println("need split:", bts)
	for uint32(len(bts)) > s.frameSize {
		frame := newFrame(cmd, sid)
		frame.data = make([]byte, s.frameSize)
		n := copy(frame.data, bts)
		bts = bts[n:]
		frames = append(frames, frame)
	}
	if len(bts) > 0 {
		frame := newFrame(cmd, sid)
		frame.data = make([]byte, len(bts))
		copy(frame.data, bts)
		frames = append(frames, frame)
	}
	return
}
