package gateway

import (
	"errors"
	"path/filepath"
	"strings"
	"time"
	_ "time/tzdata"
)

var overviewCards = []string{"info", "traffic", "https", "connections", "icmp", "service-host", "service-xray", "service-warp", "service-wireguard"}
var rangeUnits = map[string]int{"minute": 1, "hour": 60, "day": 1440, "month": 43200, "year": 525600}

func defaultPresentation() M {
	p := M{"home_header_auto_hide": false, "home_header_idle_seconds": 5, "overview_show_clock": false, "overview_show_data_status": false, "overview_color_schemes": M{}, "clock_source": "internet", "clock_server": "", "clock_timezone": "Asia/Shanghai", "nodes_limit": 0, "nodes_visibility_mode": "all", "nodes_visible_ids": []any{}, "service_custom_themes": []any{}, "overview_text": M{}, "overview_surfaces": M{}, "content_width_percent": 95, "module_width_percent": 95, "range_value": 24, "range_unit": "hour", "default_overview_view": "services", "services_host_style": "radial", "services_xray_style": "metrics", "services_warp_style": "radial-diamond-compact", "services_wireguard_layout": "metrics", "services_xray_order": []any{"upload", "download", "latency", "connections"}, "services_warp_order": []any{"upload", "download", "latency", "connections"}, "services_style": "metrics", "services_wireguard_count": 2, "services_wireguard_axis": "auto", "services_wireguard_diameter": 0, "services_wireguard_label_size": 14, "services_wireguard_value_size": 24, "services_wireguard_ring_opacity": 100, "services_wireguard_ring_shadow": 0, "services_wireguard_ring_color": "", "services_wireguard_style": "above", "services_host_order": []any{"cpu", "load", "memory", "root"}, "services_wireguard_interfaces": []any{}, "services_wireguard_metric_orders": M{}, "nodes_layout": "grid", "nodes_order": []any{}, "overview_independent": false, "overview_range_value": 24, "overview_range_unit": "hour", "services_layout": "top", "services_cards": []any{"service-host", "service-xray", "service-warp", "service-wireguard"}, "overview_layout": "grid", "overview_cards": []any{"info", "traffic", "https", "connections"}}
	for _, module := range []string{"xray", "warp", "wireguard"} {
		for view, sections := range monitorSectionOptions {
			p["monitor_"+module+"_"+view+"_sections"] = clone(sections)
			p["monitor_"+module+"_"+view+"_layout"] = "single"
		}
	}
	return p
}

var monitorSectionOptions = map[string][]any{
	"traffic":     {"trend", "statistics"},
	"latency":     {"trend", "recent"},
	"connections": {"trend", "details"},
}

func (a *App) presentationPolicy() M {
	a.mu.RLock()
	defer a.mu.RUnlock()
	p := defaultPresentation()
	for k, v := range obj(a.settings["presentation"]) {
		p[k] = clone(v)
	}
	seedServicePresentation(p, obj(a.settings["presentation"]))
	return p
}
func seedServicePresentation(p, saved M) {
	if saved["services_cards"] != nil {
		return
	}
	cards := arr(saved["overview_cards"])
	if len(cards) == 0 {
		return
	}
	for _, v := range cards {
		if !strings.HasPrefix(str(v), "service-") {
			return
		}
	}
	p["services_cards"] = clone(cards)
	if saved["services_layout"] == nil && saved["overview_layout"] != nil {
		p["services_layout"] = saved["overview_layout"]
	}
}
func validOrder(value any, allowed []string, complete bool) bool {
	rows, ok := value.([]any)
	if !ok {
		return false
	}
	if complete && len(rows) != len(allowed) || len(rows) > len(allowed) {
		return false
	}
	seen := map[string]bool{}
	for _, v := range rows {
		s, ok := v.(string)
		if !ok || seen[s] {
			return false
		}
		found := false
		for _, key := range allowed {
			if key == s {
				found = true
			}
		}
		if !found {
			return false
		}
		seen[s] = true
	}
	return true
}
func (a *App) savePresentation(input M) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	p := defaultPresentation()
	for k, v := range obj(a.settings["presentation"]) {
		if _, ok := p[k]; ok {
			p[k] = clone(v)
		}
	}
	seedServicePresentation(p, obj(a.settings["presentation"]))
	for k, v := range input {
		if _, ok := p[k]; !ok {
			return errors.New("页面布局设置包含未知字段")
		}
		p[k] = v
	}
	if err := validatePresentation(p); err != nil {
		return err
	}
	next := clone(a.settings)
	next["presentation"] = p
	if err := writeJSON(filepath.Join(a.base, "manager-settings.json"), next); err != nil {
		return err
	}
	a.settings = next
	return nil
}

func validOverviewLayout(layout string, cardCount int) bool {
	switch layout {
	case "grid", "top", "sidebar":
		return true
	case "equal-rows", "equal-columns":
		return cardCount == 2
	default:
		return false
	}
}

func validatePresentation(p M) error {
	for _, key := range []string{"overview_show_clock", "overview_show_data_status", "home_header_auto_hide"} {
		if _, ok := p[key].(bool); !ok {
			return errors.New("主页显示项必须为开关值")
		}
	}
	if seconds := num(p["home_header_idle_seconds"]); seconds < 1 || seconds > 3600 || seconds != float64(int(seconds)) {
		return errors.New("首页标题栏自动隐藏时间须为 1～3600 秒的整数")
	}
	if !validClockServer(str(p["clock_server"])) {
		return errors.New("时间服务器须为不含账号、查询参数或片段的HTTPS地址")
	}
	if source := str(p["clock_source"]); source != "internet" && source != "server" && source != "browser" {
		return errors.New("时钟来源无效")
	}
	zone := str(p["clock_timezone"])
	if _, err := time.LoadLocation(zone); err != nil {
		return errors.New("时区无效")
	}
	schemes, ok := p["overview_color_schemes"].(map[string]any)
	if !ok || len(schemes) > 2 {
		return errors.New("主题配色无效")
	}
	for key, raw := range schemes {
		if key != "dark" && key != "light" {
			return errors.New("主题无效")
		}
		s, ok := raw.(map[string]any)
		if !ok {
			return errors.New("主题配色无效")
		}
		candidate := defaultPresentation()
		for k, v := range s {
			switch k {
			case "overview_surfaces", "services_wireguard_ring_color", "services_wireguard_ring_opacity", "services_wireguard_ring_shadow":
				candidate[k] = v
			default:
				return errors.New("主题包含非配色字段")
			}
		}
		if err := validatePresentation(candidate); err != nil {
			return err
		}
	}
	for _, module := range []string{"xray", "warp", "wireguard"} {
		for view, sections := range monitorSectionOptions {
			layout := str(p["monitor_"+module+"_"+view+"_layout"])
			if layout != "single" && layout != "double" {
				return errors.New("监控分区布局须为单排或双排")
			}
			allowed := []string{str(sections[0]), str(sections[1])}
			if !validOrder(p["monitor_"+module+"_"+view+"_sections"], allowed, false) {
				return errors.New("监控分区重复或无效")
			}
		}
	}
	for _, key := range []string{"content_width_percent", "module_width_percent"} {
		if _, err := integer(p[key], 80, 100); err != nil {
			return errors.New("页面显示宽度须为80～100的整数百分比")
		}
	}
	for _, prefix := range []string{"", "overview_"} {
		factor, ok := rangeUnits[str(p[prefix+"range_unit"])]
		if !ok {
			return errors.New("时间范围单位无效")
		}
		n, err := integer(p[prefix+"range_value"], 1, 525600)
		if err != nil || n*factor > 525600 {
			return errors.New("默认时间范围须为1分钟～1年")
		}
	}
	if _, ok := p["overview_independent"].(bool); !ok {
		return errors.New("主页独立时间范围必须为开关值")
	}
	if s := str(p["default_overview_view"]); s != "dashboard" && s != "services" && s != "nodes" {
		return errors.New("主页默认样式无效")
	}
	if _, err := integer(p["nodes_limit"], 0, 4096); err != nil {
		return errors.New("节点显示数量须为0～4096")
	}
	if s := str(p["nodes_visibility_mode"]); s != "all" && s != "selected" {
		return errors.New("节点显示范围无效")
	}
	visibleIDs, ok := p["nodes_visible_ids"].([]any)
	if !ok || len(visibleIDs) > 4096 {
		return errors.New("显示节点列表无效")
	}
	seenNodes := map[string]bool{}
	for _, raw := range visibleIDs {
		id, ok := raw.(string)
		if !ok || id == "" || len(id) > 128 || strings.ContainsAny(id, "\r\n\x00") || seenNodes[id] {
			return errors.New("显示节点重复或无效")
		}
		seenNodes[id] = true
	}
	if s := str(p["nodes_layout"]); s != "grid" && s != "top" && s != "sidebar" && s != "single" {
		return errors.New("节点一览排列方式无效")
	}
	if s := str(p["services_style"]); s != "metrics" && s != "metrics-left" && s != "radial" && s != "radial-diamond" && s != "radial-diamond-compact" && s != "radial-wave" && s != "radial-lightning" && s != "radial-reverse-lightning" && s != "radial-square" && s != "radial-mixed" {
		return errors.New("服务总览卡片样式无效")
	}
	for _, key := range []string{"services_host_style", "services_xray_style", "services_warp_style", "services_wireguard_layout"} {
		v := str(p[key])
		if v == "radial-vertical" && key != "services_wireguard_layout" && str(p["services_layout"]) != "sidebar" {
			return errors.New("四圆竖排仅用于左右布局")
		}
		if _, ok := p[key].(string); !ok || (v != "" && v != "metrics" && v != "metrics-left" && v != "radial" && v != "radial-diamond" && v != "radial-diamond-compact" && v != "radial-wave" && v != "radial-lightning" && v != "radial-reverse-lightning" && v != "radial-square" && v != "radial-mixed" && v != "radial-vertical") {
			return errors.New("服务卡片独立样式无效")
		}
	}
	for _, key := range []string{"services_xray_order", "services_warp_order"} {
		if !validOrder(p[key], []string{"upload", "download", "latency", "connections"}, true) {
			return errors.New("服务指标顺序须包含上传、下载、延迟、连接数各一次")
		}
	}
	if _, err := integer(p["services_wireguard_count"], 1, 4); err != nil {
		return errors.New("显示接口数量须为1～4")
	}
	if axis := str(p["services_wireguard_axis"]); axis != "auto" && axis != "row" && axis != "column" {
		return errors.New("WireGuard圆形排列无效")
	}
	if d, err := integer(p["services_wireguard_diameter"], 0, 1200); err != nil || (d > 0 && d < 40) {
		return errors.New("圆形直径须为0或40～1200")
	}
	if _, err := integer(p["services_wireguard_label_size"], 8, 24); err != nil {
		return errors.New("WireGuard指标名称字号须为8～24")
	}
	if _, err := integer(p["services_wireguard_value_size"], 12, 40); err != nil {
		return errors.New("WireGuard指标数值字号须为12～40")
	}
	if _, err := integer(p["services_wireguard_ring_opacity"], 0, 100); err != nil {
		return errors.New("圆环不透明度须为0～100")
	}
	if _, err := integer(p["services_wireguard_ring_shadow"], 0, 40); err != nil {
		return errors.New("圆环阴影须为0～40")
	}
	if color, ok := p["services_wireguard_ring_color"].(string); !ok || (color != "" && !validPresentationColor(color)) {
		return errors.New("WireGuard圆环颜色须为六位十六进制颜色")
	}
	if s := str(p["services_wireguard_style"]); s != "center" && s != "classic" && s != "corner" && s != "corner-left" && s != "above" {
		return errors.New("WireGuard圆形样式无效")
	}
	if !validOrder(p["services_host_order"], []string{"cpu", "load", "memory", "root"}, true) {
		return errors.New("服务器圆形指标顺序须包含CPU、负载、内存、分区各一次")
	}
	metricOrders, ok := p["services_wireguard_metric_orders"].(map[string]any)
	if !ok || len(metricOrders) > 128 {
		return errors.New("WireGuard 指标顺序须按接口保存，最多128个接口")
	}
	for name, order := range metricOrders {
		if iface(name) != nil || !validOrder(order, []string{"upload", "download", "latency", "connections"}, true) {
			return errors.New("WireGuard 接口名称或指标顺序无效，每项指标须且仅须出现一次")
		}
	}
	interfaces, ok := p["services_wireguard_interfaces"].([]any)
	if !ok || len(interfaces) > 4 {
		return errors.New("WireGuard主页最多选择四个接口")
	}
	interfaceSeen := map[string]bool{}
	for _, value := range interfaces {
		name, ok := value.(string)
		if !ok || name == "" || len(name) > 64 || strings.ContainsAny(name, "\r\n\x00") || interfaceSeen[name] {
			return errors.New("WireGuard主页接口重复或无效")
		}
		interfaceSeen[name] = true
	}
	order, ok := p["nodes_order"].([]any)
	if !ok || len(order) > 4096 {
		return errors.New("节点卡片顺序无效")
	}
	seen := map[string]bool{}
	for _, v := range order {
		id, ok := v.(string)
		if !ok || id == "" || id == "warp" || len(id) > 128 || strings.ContainsAny(id, "\r\n\x00") || seen[id] {
			return errors.New("节点卡片顺序重复或无效")
		}
		seen[id] = true
	}
	if !validOverviewLayout(str(p["overview_layout"]), len(arr(p["overview_cards"]))) {
		return errors.New("主页排列方式无效")
	}
	if len(arr(p["overview_cards"])) > 4 {
		return errors.New("主页最多选择4个卡片")
	}
	if !validOrder(p["overview_cards"], overviewCards, false) {
		return errors.New("主页卡片重复或无效")
	}
	if !validOverviewLayout(str(p["services_layout"]), len(arr(p["services_cards"]))) {
		return errors.New("服务总览排列方式无效")
	}
	if !validOrder(p["services_cards"], []string{"service-host", "service-xray", "service-warp", "service-wireguard"}, false) {
		return errors.New("服务总览卡片重复或无效")
	}
	if !validOverviewText(p["overview_text"]) {
		return errors.New("指标名称字号须为8～24，数值字号须为12～40的整数")
	}
	if !validOverviewSurfaces(p["overview_surfaces"]) {
		return errors.New("主页卡片外观无效：颜色使用六位十六进制，不透明度0～100，模糊0～40")
	}

	themes, ok := p["service_custom_themes"].([]any)
	if !ok || len(themes) > 20 {
		return errors.New("自定义主题最多保存20个")
	}
	ids, names := map[string]bool{}, map[string]bool{}
	allowed := map[string]bool{}
	for _, key := range []string{"overview_color_schemes", "services_layout", "services_cards", "services_style", "services_host_style", "services_xray_style", "services_warp_style", "services_wireguard_layout", "services_wireguard_style", "services_wireguard_count", "services_wireguard_axis", "services_wireguard_diameter", "services_wireguard_ring_color", "services_wireguard_label_size", "services_wireguard_value_size", "services_wireguard_ring_opacity", "services_wireguard_ring_shadow", "services_host_order", "services_xray_order", "services_warp_order", "services_wireguard_interfaces", "services_wireguard_metric_orders", "overview_text", "overview_surfaces"} {
		allowed[key] = true
	}
	for _, raw := range themes {
		theme, ok := raw.(map[string]any)
		if !ok || len(theme) != 3 {
			return errors.New("自定义主题结构无效")
		}
		id, name := str(theme["id"]), str(theme["name"])
		if !strings.HasPrefix(id, "custom-") || len(id) > 128 || strings.TrimSpace(name) != name || name == "" || len([]rune(name)) > 40 || strings.ContainsAny(id+name, "\r\n\x00") || ids[id] || names[name] {
			return errors.New("自定义主题名称或编号重复、无效")
		}
		ids[id], names[name] = true, true
		values, ok := theme["values"].(map[string]any)
		if !ok || len(values) == 0 {
			return errors.New("自定义主题内容无效")
		}
		candidate := defaultPresentation()
		for k, v := range values {
			if !allowed[k] {
				return errors.New("自定义主题包含无关设置")
			}
			candidate[k] = clone(v)
		}
		if err := validatePresentation(candidate); err != nil {
			return err
		}
	}
	return nil
}

func validPresentationColor(color string) bool {
	if len(color) != 7 || color[0] != '#' {
		return false
	}
	for _, c := range color[1:] {
		if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
			return false
		}
	}
	return true
}
