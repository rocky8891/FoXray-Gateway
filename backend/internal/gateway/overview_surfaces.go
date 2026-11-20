package gateway

import "regexp"

var surfaceColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func validOverviewSurfaces(value any) bool {
	surfaces, ok := value.(map[string]any)
	if !ok {
		return false
	}
	for layer, value := range surfaces {
		if layer != "container" && layer != "card" && layer != "detail" {
			return false
		}
		s, ok := value.(map[string]any)
		if !ok {
			return false
		}
		for key := range s {
			if key != "enabled" && key != "color" && key != "opacity" && key != "blur" && key != "border_enabled" && key != "border_visible" && key != "module_borders" && key != "border_color" && key != "border_opacity" && key != "shadow" {
				return false
			}
		}
		if _, ok := s["enabled"].(bool); !ok || !surfaceColor.MatchString(str(s["color"])) {
			return false
		}
		if _, err := integer(s["opacity"], 0, 100); err != nil {
			return false
		}
		if _, err := integer(s["blur"], 0, 40); err != nil {
			return false
		}
		if v, exists := s["border_enabled"]; exists {
			if _, ok := v.(bool); !ok {
				return false
			}
		}
		if v, exists := s["border_visible"]; exists {
			if _, ok := v.(bool); !ok {
				return false
			}
		}
		if value, exists := s["module_borders"]; exists {
			borders, ok := value.(map[string]any)
			if !ok {
				return false
			}
			for key, value := range borders {
				if key != "host" && key != "xray" && key != "warp" && key != "wireguard" {
					return false
				}
				if _, ok := value.(bool); !ok {
					return false
				}
			}
		}
		if v, exists := s["border_color"]; exists && !surfaceColor.MatchString(str(v)) {
			return false
		}
		if v, exists := s["border_opacity"]; exists {
			if _, err := integer(v, 0, 100); err != nil {
				return false
			}
		}
		if v, exists := s["shadow"]; exists {
			if _, err := integer(v, 0, 40); err != nil {
				return false
			}
		}

	}
	return true
}

func validOverviewText(value any) bool {
	m, ok := value.(map[string]any)
	if !ok {
		return false
	}
	for key, value := range m {
		low, high := 8, 24
		if key == "value_size" {
			low, high = 12, 40
		} else if key != "label_size" {
			return false
		}
		if _, err := integer(value, low, high); err != nil {
			return false
		}
	}
	return true
}
