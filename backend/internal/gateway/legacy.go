package gateway

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
)

func (a *App) legacyDiagnostic(identity string) M {
	path := filepath.Join(a.base, "private", "diagnostic-history.sqlite3")
	if !regular(path) {
		return nil
	}
	db, e := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?mode=ro")
	if e != nil {
		return nil
	}
	defer db.Close()
	var raw, request string
	if db.QueryRow("SELECT request,result FROM runs WHERE id=?", identity).Scan(&request, &raw) != nil {
		return nil
	}
	m := M{}
	if json.Unmarshal([]byte(raw), &m) != nil {
		return nil
	}
	params := M{}
	if json.Unmarshal([]byte(request), &params) == nil {
		m["parameters"] = params
	}
	return m
}
func (a *App) diagnosticHistory(before string, diagnosticKind ...string) M {
	current := a.operations(before, "diagnostic.", diagnosticKind...)
	if len(diagnosticKind) > 0 && diagnosticKind[0] == "firewall" {
		return current
	}
	items, _ := current["items"].([]M)
	path := filepath.Join(a.base, "private", "diagnostic-history.sqlite3")
	if regular(path) {
		db, e := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?mode=ro")
		if e == nil {
			defer db.Close()
			rows, e := db.Query("SELECT id,t,request,result FROM runs ORDER BY t DESC LIMIT 30")
			if e == nil {
				defer rows.Close()
				for rows.Next() {
					var identity, request, result string
					var stamp float64
					if rows.Scan(&identity, &stamp, &request, &result) != nil {
						continue
					}
					params, data := M{}, M{}
					_ = json.Unmarshal([]byte(request), &params)
					_ = json.Unmarshal([]byte(result), &data)
					status := "success"
					if str(data["status"]) == "error" {
						status = "error"
					}
					items = append(items, M{"id": "legacy-" + identity, "time": stamp, "action": "diagnostic.result", "status": status, "message": first(data["message"], "历史诊断"), "details": M{"parameters": params, "legacy_result": data}})
				}
			}
		}
	}
	current["items"] = items
	return current
}
func (a *App) diagnosticParams(identity string) M {
	if strings.HasPrefix(identity, "legacy-") {
		result := a.legacyDiagnostic(strings.TrimPrefix(identity, "legacy-"))
		if result == nil {
			return nil
		}
		return obj(first(result["request"], result["parameters"]))
	}
	row := a.operation(identity)
	if row == nil || (row["action"] != "diagnostic.result" && row["action"] != "diagnostic.finished") {
		return nil
	}
	return obj(obj(row["details"])["parameters"])
}
