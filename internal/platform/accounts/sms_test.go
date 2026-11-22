package accounts

import (
	"context"
	"testing"
)

type recordMailer struct{ to, subject, body string }

func (r *recordMailer) Send(_ context.Context, to []string, subject, body string) error {
	r.to, r.subject, r.body = to[0], subject, body
	return nil
}

// AH-280 AC-N3: mailhog chỉ ở môi trường thử; mặc định tắt; giá trị lạ hoặc thiếu SMTP → lỗi khởi động.
func TestSMSFromEnv(t *testing.T) {
	env := func(kv ...string) func(string) string {
		m := map[string]string{}
		for i := 0; i+1 < len(kv); i += 2 {
			m[kv[i]] = kv[i+1]
		}
		return func(k string) string { return m[k] }
	}
	mailer := &recordMailer{}
	for name, tc := range map[string]struct {
		getenv func(string) string
		mailer *recordMailer
		on     bool
		fail   bool
	}{
		"mặc định tắt":             {env(), mailer, false, false},
		"disabled":                 {env("AG_SMS", "disabled"), mailer, false, false},
		"mailhog ở môi trường thử": {env("AG_SMS", "mailhog", "AG_ENV", "test"), mailer, true, false},
		"mailhog ở production":     {env("AG_SMS", "mailhog", "AG_ENV", "production"), mailer, false, true},
		"mailhog thiếu SMTP":       {env("AG_SMS", "mailhog", "AG_ENV", "dev"), nil, false, true},
		"giá trị lạ":               {env("AG_SMS", "viettel"), mailer, false, true},
	} {
		t.Run(name, func(t *testing.T) {
			var m interface {
				Send(context.Context, []string, string, string) error
			}
			if tc.mailer != nil {
				m = tc.mailer
			}
			var sender any
			var err error
			if m == nil {
				sender, err = SMSFromEnv(tc.getenv, nil)
			} else {
				sender, err = SMSFromEnv(tc.getenv, tc.mailer)
			}
			if (err != nil) != tc.fail || (sender != nil && sender != any(nil)) != tc.on {
				t.Fatalf("sender=%v err=%v", sender, err)
			}
		})
	}
	s, err := SMSFromEnv(env("AG_SMS", "mailhog", "AG_ENV", "local"), mailer)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SendSMS(context.Background(), "0913456222", "Ma OTP 123456"); err != nil {
		t.Fatal(err)
	}
	if mailer.to != "sms@htx.test" || mailer.subject != "SMS 0913456222" || mailer.body != "Ma OTP 123456" {
		t.Fatalf("mailhog mail: %+v", mailer)
	}
}
