package main

import (
	"math"
	"strings"
	"time"
)

func validClock(s string) bool {
	_, err := time.ParseInLocation("20060102150405", s, zone)
	return len(s) == 14 && err == nil
}
func basicID(id string) bool {
	if !digits(id, 41) || !validClock(id[22:36]) {
		return false
	}
	switch id[20:22] {
	case "01", "02", "03", "99":
		return true
	}
	return false
}
func imageID(id string) bool { return basicID(id) && id[20:22] == "02" }
func validateCapture(t objectType, obj M) error {
	id, _ := obj[t.id].(string)
	src, _ := obj["SourceID"].(string)
	dev, _ := obj["DeviceID"].(string)
	subtype := map[string]string{"person": "01", "motor": "02", "nonmotor": "03", "face": "06"}[t.kind]
	if !digits(dev, 20) || !basicID(src) || !digits(id, 48) || id[:41] != src || id[41:43] != subtype {
		return bad("设备、来源或结构化对象编号不符合 2017 编码")
	}
	if n, ok := obj["InfoKind"].(float64); !ok || n != 1 {
		return bad("当前严格接入支持自动采集 InfoKind=1")
	}
	for _, pair := range [][2]string{{"LeftTopX", "RightBtmX"}, {"LeftTopY", "RightBtmY"}} {
		lo, ok1 := obj[pair[0]].(float64)
		hi, ok2 := obj[pair[1]].(float64)
		if !ok1 || !ok2 || lo < 0 || hi < lo || hi > math.MaxInt32 || lo != math.Trunc(lo) || hi != math.Trunc(hi) {
			return bad("对象边界坐标无效")
		}
	}
	for _, k := range []string{"ShotTime", "PassTime", "LocationMarkTime", "MarkTime", "AppearTime", "DisappearTime", t.object + "AppearTime", t.object + "DisAppearTime"} {
		if v, ok := obj[k]; ok {
			clock, ok := v.(string)
			if !ok || !validClock(clock) {
				return bad(k + " 应为有效的 14 位日期时间")
			}
		}
	}
	if g, ok := obj["GenderCode"]; ok {
		v, ok := g.(string)
		if !ok || len(v) != 1 || !strings.Contains("0129", v) {
			return bad("GenderCode 无效")
		}
	}
	if t.kind == "face" {
		for _, k := range []string{"IsSuspectedTerrorist", "IsCriminalInvolved", "IsVictim", "IsSuspiciousPerson"} {
			n, ok := obj[k].(float64)
			if !ok || n < 0 || n > 2 || n != math.Trunc(n) {
				return bad(k + " 应为 0、1 或 2")
			}
		}
	}
	if t.kind == "motor" || t.kind == "nonmotor" {
		for _, k := range []string{"VehicleColor", "PlateClass", "PlateColor", "PlateNo"} {
			v, ok := obj[k].(string)
			if !ok || strings.TrimSpace(v) == "" {
				return bad("缺少有效的 " + k)
			}
		}
		switch v := obj["HasPlate"].(type) {
		case bool:
		case float64:
			if v != 0 && v != 1 && v != 2 {
				return bad("HasPlate 无效")
			}
		default:
			return bad("HasPlate 无效")
		}
		if t.kind == "motor" && field(obj, "StorageUrl1") == "" {
			return bad("自动采集机动车缺少近景图 StorageUrl1")
		}
	}
	if group, exists := obj["SubImageList"]; exists {
		m, ok := group.(map[string]any)
		list, okList := m["SubImageInfoObject"].([]any)
		if !ok || !okList {
			return bad("子图像列表格式无效")
		}
		for _, item := range list {
			pic, ok := item.(map[string]any)
			if !ok || !imageID(field(pic, "ImageID")) {
				return bad("子图像编号无效")
			}
			if d := field(pic, "DeviceID"); d != "" && d != dev {
				return bad("子图像设备编号冲突")
			}
		}
	}
	return nil
}
func businessID(id, kind string) bool {
	return digits(id, 33) && strings.HasPrefix(id[12:], kind) && validClock(id[14:28])
}
