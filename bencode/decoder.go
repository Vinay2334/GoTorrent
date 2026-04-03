package bencode

import (
	"bufio"
	"errors"
	"io"
	"strconv"
)

type decoder struct {
	bufio.Reader
}

func (d *decoder) readIntUntil(delimiter byte) (interface{}, error) {
	res, err := d.ReadSlice(delimiter)
	if err != nil {
		return nil, err
	}
	str := string(res[:len(res)-1])
	if value, err := strconv.ParseInt(str, 10, 64); err == nil {
		return value, err
	} else if value, err := strconv.ParseUint(str, 10, 64); err == nil {
		return value, err
	} else {
		return nil, err
	}
}

func (d *decoder) readString() (string, error) {
	len, err := d.readIntUntil(':')
	if err != nil {
		return "", err
	}
	var strLength int64
	var ok bool
	if strLength, ok = len.(int64); !ok {
		return "", errors.New("invalid string length")
	}
	if strLength < 0 {
		return "", errors.New("negative string length")
	}
	strBytes := make([]byte, strLength)
	_, err = io.ReadFull(d, strBytes)
	return string(strBytes), err
}

func (d *decoder) readInt() (interface{}, error) {
	return d.readIntUntil('e')
}

func (d *decoder) readList() ([]interface{}, error) {
	var list []interface{}
	for {
		ch, err := d.ReadByte()
		if err != nil {
			return nil, err
		}
		if ch == 'e' {
			break
		}
		item, err := d.readInterfaceType(ch)
		if err != nil {
			return nil, err
		}
		list = append(list, item)
	}
	return list, nil
}

func (d *decoder) readInterfaceType(identifier byte) (item interface{}, err error) {
	switch identifier {
	case 'i':
		item, err = d.readInt()
	case 'l':
		item, err = d.readList()
	case 'd':
		item, err = d.readDictionary()
	default:
		if err := d.UnreadByte(); err != nil {
			return nil, err
		}
		item, err = d.readString()
	}
	return item, err
}

func (d *decoder) readDictionary() (map[string]interface{}, error) {
	result := make(map[string]interface{})
	for {
		key,err := d.readString()
		if err != nil {
			return nil, err
		}
		ch, err := d.ReadByte()
		if err != nil {
			return nil, err
		}

		item, err := d.readInterfaceType(ch)
		if err != nil {
			return nil, err
		}

		result[key] = item
		nextByte, err := d.ReadByte()
		if err != nil {
			return nil, err
		}

		if nextByte == 'e' {
			break
		} else if err := d.UnreadByte(); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func Decode(reader io.Reader) (map[string]interface{}, error) {
	decoder := decoder{*bufio.NewReader(reader)}
	if firstByte, err := decoder.ReadByte(); err != nil {
		return make(map[string]interface{}), err
	} else if firstByte != 'd' {
		return make(map[string]interface{}), errors.New("invalid bencode format: expected 'd' at the beginning")
	}
	return decoder.readDictionary()
}