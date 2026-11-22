package main

import (
	"net/url"
	"strconv"
	"strings"
)

// Translate the equality and pagination subset of GA/T 1400.3 Annex D.
// Reject unsupported conditions instead of accidentally returning all objects.
func objectQuery(input url.Values, objectKey string) (url.Values, error) {
	out := url.Values{}
	for key, values := range input {
		if len(values) != 1 {
			return nil, bad("不支持重复查询条件")
		}
		k, v := strings.TrimSpace(key), strings.TrimSpace(values[0])
		if strings.HasPrefix(k, "(") {
			if !strings.HasSuffix(v, ")") {
				return nil, bad("查询括号不匹配")
			}
			k = strings.TrimSpace(k[1:])
			v = strings.TrimSpace(v[:len(v)-1])
		}
		switch k {
		case "page", "size", "q", "start", "end", "device", "Name", "PlateNo":
			out.Set(k, v)
		case "DeviceID":
			out.Set("device", v)
		case objectKey:
			out.Set("exactID", v)
		case "PageRecordNum", "RecordStartNo":
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 {
				return nil, bad("分页参数须为正整数")
			}
			if k == "PageRecordNum" {
				out.Set("size", v)
			} else {
				out.Set("startRecord", v)
			}
		default:
			return nil, bad("暂不支持该查询条件")
		}
	}
	return out, nil
}
