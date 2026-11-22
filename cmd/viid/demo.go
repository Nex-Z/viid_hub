package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
)

// seed is opt-in and runs as one transaction only when the device catalog is empty.
func (a *app) seed(ctx context.Context) error {
	tx, e := a.db.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(140001)"); e != nil {
		return e
	}
	var exists bool
	if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM devices)").Scan(&exists); e != nil || exists {
		return e
	}
	now := time.Now().Truncate(time.Second)
	names := []string{"东门人行通道", "南门广场", "主干道卡口", "西侧通道", "北门出入口", "商业街入口", "地铁站东口", "公交站台", "东门广场", "停车场入口", "中山路卡口", "人民路辅道"}
	brands := []string{"海康", "大华", "宇视"}
	kinds := []string{"人脸抓拍机", "人员采集机", "机动车卡口", "非机动车卡口"}
	batch := &pgx.Batch{}
	for i, p := range names {
		id := fmt.Sprintf("5001030000132%07d", i+1)
		beat := now.Add(-time.Duration(i*3) * time.Second)
		if i == 9 {
			beat = beat.Add(-time.Hour)
		}
		batch.Queue("INSERT INTO devices(id,name,kind,ip,point,registered,heartbeat) VALUES($1,$2,$3,$4,$5,$6,$7)", id, brands[i%3]+" · "+p, kinds[i%4], fmt.Sprintf("192.168.10.%d", 21+i), p, i != 11, beat)
		for j := 0; j < 5; j++ {
			batch.Queue("INSERT INTO device_events(device_id,event,at) VALUES($1,'心跳',$2)", id, beat.Add(-time.Duration(j)*time.Minute))
		}
	}
	if e = tx.SendBatch(ctx, batch).Close(); e != nil {
		return e
	}
	assets := map[string][][]byte{}
	for kind, names := range map[string][]string{"person": {"person1", "person2", "person3", "person4"}, "face": {"person4", "person2", "person3", "person1"}, "motor": {"car1", "car2", "car3"}, "nonmotor": {"bike1", "bike2", "bike3"}} {
		for _, name := range names {
			b, e := os.ReadFile("demo/media/" + name + ".jpg")
			if e != nil {
				return e
			}
			assets[kind] = append(assets[kind], b)
		}
	}
	people := []string{"陈一", "林悦", "徐安", "周宁", "李晨", "张琪", "王远", "陆嘉"}
	i := 0
	for hour := 0; hour < 48; hour++ {
		n := 8 + int(16*(1+math.Sin(float64(hour)*0.27+1))) + hour%5
		for j := 0; j < n; j++ {
			kind := []string{"person", "motor", "nonmotor", "face", "motor", "person", "motor"}[i%7]
			id := fmt.Sprintf("DEMO-%s-%06d", kind, i+1)
			device := fmt.Sprintf("5001030000132%07d", i%len(names)+1)
			at := now.Add(-time.Duration(hour)*time.Hour - time.Duration(j*3600/n+10)*time.Second)
			raw := M{"DeviceID": device, "ShotTime": stamp(at), "Source": "演示样本"}
			var title string
			switch kind {
			case "person", "face":
				title = people[(i/3)%len(people)]
				raw["Name"] = title
				raw["GenderCode"] = []string{"男", "女"}[(i/3)%2]
				raw["Age"] = 24 + i%26
				raw["IDNumber"] = fmt.Sprintf("500103********%04d", i%9999)
			case "motor":
				title = fmt.Sprintf("渝A·%05X", (i*2017+0x1268)%0xFFFFF)
				raw["PlateNo"] = title
				raw["VehicleClass"] = []string{"小型轿车", "小型SUV", "出租车"}[i%3]
				raw["Speed"] = 36 + i%39
			default:
				title = []string{"电动自行车", "自行车", "电动三轮车"}[i%3]
				raw["VehicleClass"] = title
				raw["RiderCount"] = 1 + i%2
			}
			if kind == "motor" || kind == "nonmotor" {
				raw["VehicleColor"] = []string{"白色", "黑色", "蓝色", "灰色"}[i%4]
				raw["Direction"] = []string{"由西向东", "由北向南", "由东向西", "由南向北"}[i%4]
			}
			for _, t := range types {
				if t.kind == kind {
					raw[t.id] = id
				}
			}
			b, _ := json.Marshal(raw)
			if _, e = tx.Exec(ctx, "INSERT INTO records(id,device_id,kind,captured,title,raw) VALUES($1,$2,$3,$4,$5,$6)", id, device, kind, at, title, b); e != nil {
				return e
			}
			photo := assets[kind][(i/3)%len(assets[kind])]
			if e = putImage(ctx, tx, "IMG-"+id, id+".jpg", id, device, kind, photo, at); e != nil {
				return e
			}
			i++
		}
	}
	return tx.Commit(ctx)
}
