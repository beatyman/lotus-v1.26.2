package utils

import (
	"encoding/binary"
	"encoding/json"
	"net"
	"strconv"

	"github.com/gwaylib/errors"
	"github.com/gwaylib/log"
)

var (
	ErrEOF        = errors.New("EOF")
	ErrFUseProto  = errors.New("fuse protocol error")
	ErrFUseParams = errors.New("fuse params error")
	ErrFUseClosed = errors.New("fuse closed")
)

const (
	FUSE_REQ_CONTROL_TEXT       = byte(0)
	FUSE_REQ_CONTROL_FILE_CLOSE = byte(1)
	FUSE_REQ_CONTROL_FILE_WRITE = byte(2)
	FUSE_REQ_CONTROL_FILE_TRUNC = byte(3)
	FUSE_REQ_CONTROL_FILE_STAT  = byte(4)
	FUSE_REQ_CONTROL_FILE_READ  = byte(5)
	FUSE_REQ_CONTROL_FILE_CAP   = byte(10)

	FUSE_RESP_CONTROL_TEXT          = byte(0)
	FUSE_RESP_CONTROL_FILE_TRANSFER = byte(1)
)

func ReadFUseReqHeader(conn net.Conn) (byte, int, error) {
	// byte 0 for signal protocal, value means:
	// value 0, control mode;
	// value 1, close file mode
	// value 2, write file mode
	// value 3, truncate file mode
	// value 4, filestat file mode
	// value 5, read file mode
	//
	// byte[8] data length, zero for ignore.
	// byte[n], data body
	controlB := make([]byte, 1)
	n, err := conn.Read(controlB)
	if err != nil {
		if !ErrEOF.Equal(err) {
			return 0, 0, errors.As(err)
		}
		return 0, 0, ErrFUseClosed.As(err)
	}
	buffLenB := make([]byte, 4)
	n, err = conn.Read(buffLenB)
	if err != nil {
		if !ErrEOF.Equal(err) {
			return 0, 0, errors.As(err)
		}
		return 0, 0, ErrFUseClosed.As(err)
	}
	if n != len(buffLenB) {
		return 0, 0, ErrFUseParams.As("error protocol length")
	}
	buffLen, _ := binary.Varint(buffLenB)
	return controlB[0], int(buffLen), nil
}

func WriteFUseReqHeader(conn net.Conn, control byte, buffLen int) error {
	// write the protocol control
	if _, err := conn.Write([]byte{control}); err != nil {
		if !ErrEOF.Equal(err) {
			return errors.As(err)
		}
		return ErrFUseClosed.As(err)
	}
	// write data len
	buffLenB := make([]byte, 4)
	binary.PutVarint(buffLenB, int64(buffLen))
	if _, err := conn.Write(buffLenB); err != nil {
		if !ErrEOF.Equal(err) {
			return errors.As(err)
		}
		return ErrFUseClosed.As(err)
	}
	return nil
}

func ReadFUseReqText(conn net.Conn, buffLen int) (map[string]string, error) {
	dataB := make([]byte, buffLen)
	read := 0
	for {
		n, err := conn.Read(dataB[read:])
		read += n
		if err != nil {
			if !ErrEOF.Equal(err) {
				return nil, errors.As(err)
			}
			// EOF is not expected.
			return nil, ErrFUseClosed.As(err)
		}

		if read < buffLen {
			// need fill full of the buffer.
			continue
		}
		break
	}
	proto := map[string]string{}
	if err := json.Unmarshal(dataB, &proto); err != nil {
		return nil, ErrFUseParams.As("error protocol format")
	}
	return proto, nil
}

func ReadFUseTextReq(conn net.Conn) (map[string]string, error) {
	control, buffLen, err := ReadFUseReqHeader(conn)
	if err != nil {
		return nil, errors.As(err)
	}
	if control != FUSE_REQ_CONTROL_TEXT {
		return nil, ErrFUseProto.As(control)
	}
	return ReadFUseReqText(conn, buffLen)
}

func WriteFUseTextReq(conn net.Conn, data []byte) error {
	if err := WriteFUseReqHeader(conn, FUSE_REQ_CONTROL_TEXT, len(data)); err != nil {
		return errors.As(err)
	}
	if _, err := conn.Write(data); err != nil {
		if !ErrEOF.Equal(err) {
			return errors.As(err)
		}
		// EOF is not expected.
		return ErrFUseClosed.As(err)
	}
	return nil
}

func ReadFUseRespHeader(conn net.Conn) (byte, error) {
	controlB := make([]byte, 1)
	if _, err := conn.Read(controlB); err != nil {
		if !ErrEOF.Equal(err) {
			return 0, errors.As(err)
		}
		// EOF is not expected.
		return 0, ErrFUseClosed.As(err)
	}
	return controlB[0], nil
}

// read the resp header and the body
func ReadFUseTextResp(conn net.Conn) (map[string]interface{}, error) {
	control, err := ReadFUseRespHeader(conn)
	if err != nil {
		return nil, errors.As(err)
	}
	if control != FUSE_RESP_CONTROL_TEXT {
		return nil, errors.As(err)
	}
	return ReadFUseRespText(conn)
}

// read the resp body
func ReadFUseRespText(conn net.Conn) (map[string]interface{}, error) {
	// byte for control
	// byte 0, protocol type. type 0, control protocol; type 1, transfer protocol.
	//
	// type 0, the control protocol
	// byte[8] data length, zero for ignore.
	// byte[n], data body
	//
	// type 1, transfer data
	//
	buffLenB := make([]byte, 4)
	n, err := conn.Read(buffLenB)
	if err != nil {
		if !ErrEOF.Equal(err) {
			return nil, errors.As(err)
		}
		// EOF is not expected.
		return nil, ErrFUseClosed.As(err)
	}
	if n != len(buffLenB) {
		return nil, ErrFUseProto.As("error protocol length")
	}
	buffLen, _ := binary.Varint(buffLenB)
	if buffLen == 0 {
		return map[string]interface{}{}, nil
	}
	buffB := make([]byte, buffLen)
	read := int64(0)
	for {
		n, err = conn.Read(buffB[read:])
		read += int64(n)
		if err != nil {
			if !ErrEOF.Equal(err) {
				return nil, errors.As(err)
			}
			// EOF is not expected.
			return nil, ErrFUseClosed.As(err)
		}
		if read < buffLen {
			// need fill full of the buffer.
			continue
		}
		break
	}
	proto := map[string]interface{}{}
	if err := json.Unmarshal(buffB, &proto); err != nil {
		return nil, ErrFUseParams.As("error protocol format")
	}
	return proto, nil
}

func WriteFUseTextResp(conn net.Conn, code int, data interface{}, err error) {
	p := map[string]interface{}{
		"Code": strconv.Itoa(code),
	}
	if data != nil {
		p["Data"] = data
	}
	if err != nil {
		p["Err"] = err.Error()
	}
	output, _ := json.Marshal(p)

	// write control code
	if _, err := conn.Write([]byte{FUSE_RESP_CONTROL_TEXT}); err != nil {
		log.Warn(errors.As(err))
		return
	}

	// write data len
	buffLen := make([]byte, 4)
	binary.PutVarint(buffLen, int64(len(output)))
	if _, err := conn.Write(buffLen); err != nil {
		log.Warn(errors.As(err))
		return
	}

	// write data body
	if _, err := conn.Write(output); err != nil {
		log.Warn(errors.As(err))
		return
	}
	return
}
func WriteFUseErrResp(conn net.Conn, code int, err error) {
	WriteFUseTextResp(conn, code, nil, err)
}
func WriteFUseSucResp(conn net.Conn, code int, data interface{}) {
	WriteFUseTextResp(conn, 200, data, nil)
}
