package gateway

import "errors"

func (a *App) managerServiceStatus() M {
	status := a.service("foxray")
	status["unit"] = "foxray.service"
	status["version"] = Version
	status["available"] = str(status["state"]) != "unknown"
	return status
}

func (a *App) managerServiceAction(data M) (M, error) {
	if a.readonly {
		return nil, errors.New("只读实例不能操作管理服务")
	}
	for key := range data {
		if key != "action" && key != "confirmed" {
			return nil, errors.New("管理服务操作包含不支持字段")
		}
	}
	action := str(data["action"])
	if action != "start" && action != "stop" && action != "restart" && action != "enable" && action != "disable" {
		return nil, errors.New("管理服务操作无效")
	}
	a.mutate.Lock()
	defer a.mutate.Unlock()
	if action == "stop" || action == "restart" {
		if !yes(data["confirmed"]) {
			return nil, errors.New("请确认管理服务停止或重启期间网页将断开")
		}
		unit := "foxray-manager-control"
		_, err := a.command(10, "", "systemd-run", "--quiet", "--collect", "--unit="+unit, "--on-active=3s", "--timer-property=AccuracySec=1s", "--property=Type=oneshot", "/usr/bin/systemctl", action, "foxray.service")
		details := M{"action": action, "unit": "foxray.service", "scheduler": unit, "delay_seconds": 3, "stage": "scheduled"}
		message := "管理服务操作已安排，约 3 秒后执行；执行结果以服务状态及 systemd 日志为准"
		if err != nil {
			a.audit("manager.service."+action+".scheduled", "管理服务操作未能安排", false, details)
			return nil, err
		}
		a.audit("manager.service."+action+".scheduled", message, true, details)
		return M{"ok": true, "scheduled": true, "delay_seconds": 3, "message": message}, nil
	}
	_, err := a.command(15, "", "systemctl", action, "foxray.service")
	a.audit("manager.service."+action, "管理服务操作："+action, err == nil, M{"action": action, "unit": "foxray.service"})
	if err != nil {
		return nil, err
	}
	return M{"ok": true, "scheduled": false, "message": "管理服务操作已完成"}, nil
}
