package encode

import (
	"encoding/binary"
	"encoding/json"
	"net"
	"strconv"

	"github.com/gwaylib/errors"
)

var (
	ErrEOF          = errors.New("EOF")
	ErrSocketProto  = errors.New("fuse protocol error")
	ErrSocketParams = errors.New("fuse params error")
	ErrSocketClosed = errors.New("fuse closed")
)

const (
	SOCKET_REQ_CONTROL_TEXT = byte(1)

	// ignore 0
	SOCKET_RESP_CONTROL_TEXT = byte(1)
)

func ReadSocketReqHeader(conn net.Conn) (byte, uint32, error) {
	// byte [0] for control protocal:
	// byte[1:5] data length.
	// byte[n], data body
	controlB := make([]byte, 1)
	n, err := conn.Read(controlB)
	if err != nil {
		if !ErrEOF.Equal(err) {
			return 0, 0, errors.As(err)
		}
		return 0, 0, ErrSocketClosed.As(err)
	}
	buffLenB := make([]byte, 4)
	n, err = conn.Read(buffLenB)
	if err != nil {
		if !ErrEOF.Equal(err) {
			return 0, 0, errors.As(err)
		}
		return 0, 0, ErrSocketClosed.As(err)
	}
	if n != len(buffLenB) {
		return 0, 0, ErrSocketParams.As("error protocol length")
	}
	buffLen, _ := binary.Uvarint(buffLenB)
	return controlB[0], uint32(buffLen), nil
}

func ReadSocketReqText(conn net.Conn, buffLen uint32) (map[string]string, error) {
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
			return nil, ErrSocketClosed.As(err)
		}

		if read < buffLen {
			// need fill full of the buffer.
			continue
		}
		break
	}
	proto := map[string]string{}
	if err := json.Unmarshal(dataB, &proto); err != nil {
		return nil, ErrSocketParams.As("error protocol format")
	}
	return proto, nil
}

func ReadSocketTextReq(conn net.Conn) (map[string]string, error) {
	control, buffLen, err := ReadSocketReqHeader(conn)
	if err != nil {
		return nil, errors.As(err)
	}
	if control != SOCKET_REQ_CONTROL_TEXT {
		return nil, ErrSocketProto.As(control)
	}
	return ReadSocketReqText(conn, buffLen)
}

func WriteSocketReqHeader(conn net.Conn, control byte, buffLen uint32) error {
	// write the protocol control
	if _, err := conn.Write([]byte{control}); err != nil {
		if !ErrEOF.Equal(err) {
			return errors.As(err)
		}
		return ErrSocketClosed.As(err)
	}
	// write data len
	buffLenB := make([]byte, 4)
	binary.PutUvarint(buffLenB, uint64(buffLen))
	if _, err := conn.Write(buffLenB); err != nil {
		if !ErrEOF.Equal(err) {
			return errors.As(err)
		}
		return ErrSocketClosed.As(err)
	}
	return nil
}

func WriteSocketTextReq(conn net.Conn, data []byte) error {
	if err := WriteSocketReqHeader(conn, SOCKET_REQ_CONTROL_TEXT, uint32(len(data))); err != nil {
		return errors.As(err)
	}
	if _, err := conn.Write(data); err != nil {
		if !ErrEOF.Equal(err) {
			return errors.As(err)
		}
		// EOF is not expected.
		return ErrSocketClosed.As(err)
	}
	return nil
}

func ReadSocketRespHeader(conn net.Conn) (byte, int32, error) {
	controlB := make([]byte, 1)
	if _, err := conn.Read(controlB); err != nil {
		if !ErrEOF.Equal(err) {
			return 0, 0, errors.As(err)
		}
		// EOF is not expected.
		return 0, 0, ErrSocketClosed.As(err)
	}
	buffLenB := make([]byte, 4)
	n, err := conn.Read(buffLenB)
	if err != nil {
		if !ErrEOF.Equal(err) {
			return 0, 0, errors.As(err)
		}
		// EOF is not expected.
		return 0, 0, ErrSocketClosed.As(err)
	}
	if n != len(buffLenB) {
		return 0, 0, ErrSocketProto.As("error protocol length")
	}
	buffLen, _ := binary.Uvarint(buffLenB)
	return controlB[0], int32(buffLen), nil
}

// read the resp header and the body
func ReadSocketTextResp(conn net.Conn) (map[string]interface{}, error) {
	control, buffLen, err := ReadSocketRespHeader(conn)
	if err != nil {
		return nil, errors.As(err)
	}
	if control != SOCKET_RESP_CONTROL_TEXT {
		return nil, ErrSocketProto.As("need SOCKET_RESP_CONTROL_TEXT", control, buffLen)
	}
	return ReadSocketRespText(conn, buffLen)
}

// read the resp body
func ReadSocketRespText(conn net.Conn, buffLen int32) (map[string]interface{}, error) {
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
			return nil, ErrSocketClosed.As(err)
		}
		if read < int64(buffLen) {
			// need fill full of the buffer.
			continue
		}
		break
	}
	proto := map[string]interface{}{}
	if err := json.Unmarshal(buffB, &proto); err != nil {
		return nil, ErrSocketParams.As("error protocol format")
	}
	// checksum the protocol
	if _, ok := proto["Code"]; !ok {
		return nil, ErrSocketParams.As(buffLen, string(buffB))
	}
	return proto, nil
}

func WriteSocketRespHeader(conn net.Conn, control byte, buffLen uint32) error {
	// write the protocol control
	if _, err := conn.Write([]byte{control}); err != nil {
		if !ErrEOF.Equal(err) {
			return errors.As(err)
		}
		return ErrSocketClosed.As(err)
	}
	// write data len
	buffLenB := make([]byte, 4)
	binary.PutUvarint(buffLenB, uint64(buffLen))
	if _, err := conn.Write(buffLenB); err != nil {
		if !ErrEOF.Equal(err) {
			return errors.As(err)
		}
		return ErrSocketClosed.As(err)
	}
	return nil
}
func WriteSocketTextResp(conn net.Conn, code int, data interface{}, err error) error {
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
	if err := WriteSocketRespHeader(conn, SOCKET_RESP_CONTROL_TEXT, uint32(len(output))); err != nil {
		return errors.As(err)
	}

	// write data body
	if _, err := conn.Write(output); err != nil {
		return errors.As(err)
	}
	return nil
}
func WriteSocketErrResp(conn net.Conn, code int, err error) error {
	return WriteSocketTextResp(conn, code, nil, err)
}
func WriteSocketSucResp(conn net.Conn, code int, data interface{}) error {
	return WriteSocketTextResp(conn, code, data, nil)
}
