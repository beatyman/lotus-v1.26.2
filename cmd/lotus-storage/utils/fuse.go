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
	FUSE_REQ_CONTROL_TEXT       = byte(1)
	FUSE_REQ_CONTROL_FILE_CLOSE = byte(2)
	FUSE_REQ_CONTROL_FILE_WRITE = byte(3)
	FUSE_REQ_CONTROL_FILE_TRUNC = byte(4)
	FUSE_REQ_CONTROL_FILE_STAT  = byte(5)
	FUSE_REQ_CONTROL_FILE_READ  = byte(6)
	FUSE_REQ_CONTROL_FILE_CAP   = byte(10)

	// ignore 0
	FUSE_RESP_CONTROL_TEXT          = byte(1)
	FUSE_RESP_CONTROL_FILE_TRANSFER = byte(2)
)

func ReadFUseReqHeader(conn net.Conn) (byte, uint32, error) {
	// byte [0] for control protocal:
	// byte[1:5] data length.
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
	buffLen, _ := binary.Uvarint(buffLenB)
	return controlB[0], uint32(buffLen), nil
}

func ReadFUseReqText(conn net.Conn, buffLen uint32) (map[string]string, error) {
	dataB := make([]byte, buffLen)
	read := uint32(0)
	for {
		n, err := conn.Read(dataB[read:])
		read += uint32(n)
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

func WriteFUseReqHeader(conn net.Conn, control byte, buffLen uint32) error {
	// write the protocol control
	if _, err := conn.Write([]byte{control}); err != nil {
		if !ErrEOF.Equal(err) {
			return errors.As(err)
		}
		return ErrFUseClosed.As(err)
	}
	// write data len
	buffLenB := make([]byte, 4)
	binary.PutUvarint(buffLenB, uint64(buffLen))
	if _, err := conn.Write(buffLenB); err != nil {
		if !ErrEOF.Equal(err) {
			return errors.As(err)
		}
		return ErrFUseClosed.As(err)
	}
	return nil
}
func WriteFUseReqFileHeader(conn net.Conn, control byte, buffLen uint32, fileId [16]byte) error {
	if err := WriteFUseReqHeader(conn, control, buffLen); err != nil {
		return errors.As(err)
	}

	if _, err := conn.Write(fileId[:]); err != nil {
		return errors.As(err)
	}
	return nil
}
func WriteFUseTextReq(conn net.Conn, data []byte) error {
	if err := WriteFUseReqHeader(conn, FUSE_REQ_CONTROL_TEXT, uint32(len(data))); err != nil {
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

func ReadFUseRespHeader(conn net.Conn) (byte, int32, error) {
	controlB := make([]byte, 1)
	if _, err := conn.Read(controlB); err != nil {
		if !ErrEOF.Equal(err) {
			return 0, 0, errors.As(err)
		}
		// EOF is not expected.
		return 0, 0, ErrFUseClosed.As(err)
	}
	buffLenB := make([]byte, 4)
	n, err := conn.Read(buffLenB)
	if err != nil {
		if !ErrEOF.Equal(err) {
			return 0, 0, errors.As(err)
		}
		// EOF is not expected.
		return 0, 0, ErrFUseClosed.As(err)
	}
	if n != len(buffLenB) {
		return 0, 0, ErrFUseProto.As("error protocol length")
	}
	buffLen, _ := binary.Uvarint(buffLenB)
	return controlB[0], int32(buffLen), nil
}

// read the resp header and the body
func ReadFUseTextResp(conn net.Conn) (map[string]interface{}, error) {
	control, buffLen, err := ReadFUseRespHeader(conn)
	if err != nil {
		return nil, errors.As(err)
	}
	if control != FUSE_RESP_CONTROL_TEXT {
		return nil, ErrFUseProto.As("need FUSE_RESP_CONTROL_TEXT", control, buffLen)
	}
	return ReadFUseRespText(conn, buffLen)
}

// read the resp body
func ReadFUseRespText(conn net.Conn, buffLen int32) (map[string]interface{}, error) {
	// byte[0], protocol type. type 0, control protocol; type 1, transfer protocol.
	// byte[1:5] data length, zero for ignore.
	// byte[n], data body
	if buffLen == 0 {
		return nil, errors.New("no data response").As(buffLen)
	}
	buffB := make([]byte, buffLen)
	read := int64(0)
	for {
		n, err := conn.Read(buffB[read:])
		read += int64(n)
		if err != nil {
			if !ErrEOF.Equal(err) {
				return nil, errors.As(err)
			}
			// EOF is not expected.
			return nil, ErrFUseClosed.As(err)
		}
		if read < int64(buffLen) {
			// need fill full of the buffer.
			continue
		}
		break
	}
	proto := map[string]interface{}{}
	if err := json.Unmarshal(buffB, &proto); err != nil {
		return nil, ErrFUseParams.As("error protocol format")
	}
	// checksum the protocol
	if _, ok := proto["Code"]; !ok {
		return nil, ErrFUseParams.As(buffLen, string(buffB))
	}
	return proto, nil
}

func WriteFUseRespHeader(conn net.Conn, control byte, buffLen uint32) error {
	// write the protocol control
	if _, err := conn.Write([]byte{control}); err != nil {
		if !ErrEOF.Equal(err) {
			return errors.As(err)
		}
		return ErrFUseClosed.As(err)
	}
	// write data len
	buffLenB := make([]byte, 4)
	binary.PutUvarint(buffLenB, uint64(buffLen))
	if _, err := conn.Write(buffLenB); err != nil {
		if !ErrEOF.Equal(err) {
			return errors.As(err)
		}
		return ErrFUseClosed.As(err)
	}
	return nil
}
func WriteFUseTextResp(conn net.Conn, code int, data interface{}, err error) {
	codeStr := strconv.Itoa(code)
	p := map[string]interface{}{
		"Code": codeStr,
		"Err":  codeStr,
	}
	if data != nil {
		p["Data"] = data
	}
	if err != nil {
		p["Err"] = err.Error()
	}
	output, _ := json.Marshal(p)
	if err := WriteFUseRespHeader(conn, FUSE_RESP_CONTROL_TEXT, uint32(len(output))); err != nil {
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
