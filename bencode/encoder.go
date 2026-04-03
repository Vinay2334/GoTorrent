package bencode
import (
	"bytes"
	"fmt"
	"strconv"
	"sort"
	"reflect"
)

type encoder struct {
	bytes.Buffer
}

func (e *encoder) writeString(str string) {
	e.WriteString(fmt.Sprintf("%d:%s", len(str), str))
}

func (e *encoder) writeList(list []interface{}) {
	e.WriteByte('l')
	for _, item := range list {
		e.writeInterfaceType(item)
	}
	e.WriteByte('e')
}

func (e *encoder) writeDictionary(dict map[string]interface{}) {
	list := make(sort.StringSlice, len(dict))
	i := 0
	for key := range dict {
		list[i] = key
		i++
	}
	list.Sort()
	e.WriteByte('d')
	for _, key := range list {
		e.writeString(key)
		e.writeInterfaceType(dict[key])
	}
	e.WriteByte('e')
}

func (e *encoder) writeInt(num int64) {
	e.WriteByte('i')
	e.WriteString(strconv.FormatInt(num, 10))
	e.WriteByte('e')
}

func (e *encoder) writeUint(num uint64) {
	e.WriteByte('i')
	e.WriteString(strconv.FormatUint(num, 10))
	e.WriteByte('e')
}

func (e *encoder) writeInterfaceType(data interface{}) {
	switch v := data.(type) {
	case string:
		e.writeString(v)
	case []interface{}:
		e.writeList(v)
	case map[string]interface{}:
		e.writeDictionary(v)
	case int, int8, int16, int32, int64:
		e.writeInt(reflect.ValueOf(v).Int())
	case uint, uint8, uint16, uint32, uint64:
		e.writeUint(reflect.ValueOf(v).Uint())
	default:
		panic(fmt.Sprintf("unsupported type: %T", data))
	}
}

func Encode(data interface{}) ([]byte, error) {
	encoder := encoder{}
	encoder.writeInterfaceType(data)
	return encoder.Bytes(), nil
}