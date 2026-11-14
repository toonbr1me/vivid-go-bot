package payments

import "reflect"

// panelReady — есть ли рабочая панель.
//
// Проверка «интерфейс не nil» недостаточна: интерфейс может хранить
// nil-указатель (так получается, когда панель объявлена как *Client и не
// создана). Такая панель проходит проверку на nil, а первый же вызов падает.
// Здесь ловим оба случая.
func (s *Service) panelReady() bool {
	if s.panel == nil {
		return false
	}
	value := reflect.ValueOf(s.panel)
	switch value.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func:
		return !value.IsNil()
	default:
		return true
	}
}
