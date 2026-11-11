package app

import (
	"github.com/MEKONG-GREEN/hop-tac-xa/internal/platform/reports/port"
	"strings"
)

func (s *Service) dispatchProfile() (port.DispatchProfile, error) {
	if profile, ok := s.Policies.DispatchProfile(); ok {
		return profile, nil
	}
	return port.DispatchProfile{}, ErrDependency.WithDetail("chưa đăng ký cấu hình gửi báo cáo")
}
func dispatchFileKey(profile port.DispatchProfile, formCode string) string {
	return profile.TemplatePrefix + strings.ToLower(formCode)
}
