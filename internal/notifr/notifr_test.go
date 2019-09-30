/*
Copyright (c) JSC iCore.

This source code is licensed under the MIT license found in the
LICENSE file in the root directory of this source tree.
*/

package notifr

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/domodwyer/mailyak"
	"github.com/pkg/errors"
)

func TestTargetsConfigDecode(t *testing.T) {
	testCases := []struct {
		name        string
		targets     string
		want        *TargetsConfig
		wantErrKind valErrKind
	}{
		{
			name:    "empty target",
			targets: "",
			want:    &TargetsConfig{},
		},
		{
			name:        "without one field",
			targets:     "test:smtp",
			wantErrKind: errKindInvTargetSyntax,
		},
		{
			name:        "without two field",
			targets:     "test",
			wantErrKind: errKindInvTargetSyntax,
		},
		{
			name:        "empty first field",
			targets:     ":smtp:email@example.com",
			wantErrKind: errKindInvTargetSyntax,
		},
		{
			name:        "empty second field",
			targets:     "test::email@example.com",
			wantErrKind: errKindInvTargetSyntax,
		},
		{
			name:        "empty third field",
			targets:     "test:smtp:",
			wantErrKind: errKindInvTargetSyntax,
		},
		{
			name:        "empty fields",
			targets:     "::",
			wantErrKind: errKindInvTargetSyntax,
		},
		{
			name:    "all ok, one target",
			targets: "test:smtp:email@example.com",
			want: &TargetsConfig{
				targets: map[string]*target{
					"test": {
						deliveries: []*delivery{
							{
								name:       "smtp",
								recipients: []string{"email@example.com"},
							},
						},
					},
				},
			},
		},
		{
			name:    "all ok, two targets",
			targets: "test1:smtp:email1@example.com,test2:smtp:email2@example.com",
			want: &TargetsConfig{
				targets: map[string]*target{
					"test1": {
						deliveries: []*delivery{
							{
								name:       "smtp",
								recipients: []string{"email1@example.com"},
							},
						},
					},
					"test2": {
						deliveries: []*delivery{
							{
								name:       "smtp",
								recipients: []string{"email2@example.com"},
							},
						},
					},
				},
			},
		},
		{
			name:    "all ok, one target, two recipients",
			targets: "test:smtp:email1@example.com,test:smtp:email2@example.com",
			want: &TargetsConfig{
				targets: map[string]*target{
					"test": {
						deliveries: []*delivery{
							{
								name:       "smtp",
								recipients: []string{"email1@example.com", "email2@example.com"},
							},
						},
					},
				},
			},
		},
		{
			name:    "all ok, one target, two deliveries",
			targets: "test:smtp:email@example.com,test:sms:+79999999999",
			want: &TargetsConfig{
				targets: map[string]*target{
					"test": {
						deliveries: []*delivery{
							{
								name:       "smtp",
								recipients: []string{"email@example.com"},
							},
							{
								name:       "sms",
								recipients: []string{"+79999999999"},
							},
						},
					},
				},
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := &TargetsConfig{}
			err := got.Decode(tc.targets)
			if tc.wantErrKind != "" {
				if err == nil {
					t.Fatalf("got no error; want error kind: %v", tc.wantErrKind)
				}
				if v, ok := err.(*valError); !ok || v.kind != tc.wantErrKind {
					t.Fatalf("got error kind: %v; want error kind: %v", v.kind, tc.wantErrKind)
				}
				return
			}
			if err != nil {
				t.Fatalf("got error: %v; want no error", err)
			}

			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got config: %v; want config: %v", got, tc.want)
			}
		})
	}
}

func TestNewHandler(t *testing.T) {
	testCases := []struct {
		name                string
		targets             string
		supportedDeliveries map[DeliveryType]Sender
		wantErrKind         valErrKind
	}{
		{
			name:        "empty targets",
			targets:     "",
			wantErrKind: errKindEmptyTargets,
		},
		{
			name:                "not supported delivery type",
			targets:             "test:nosmtp:email@example.com",
			supportedDeliveries: map[DeliveryType]Sender{DeliverySMTP: nil},
			wantErrKind:         errKindUnsupportedDelivery,
		},
		{
			name:                "invalid email",
			targets:             "test:smtp:noemail",
			supportedDeliveries: map[DeliveryType]Sender{DeliverySMTP: nil},
			wantErrKind:         errKindInvalidEmail,
		},
		{
			name:                "all ok",
			targets:             "test:smtp:email@example.com",
			supportedDeliveries: map[DeliveryType]Sender{DeliverySMTP: nil},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cnf := TargetsConfig{}
			if err := cnf.Decode(tc.targets); err != nil {
				t.Fatalf("unexpected decode error: %s", err)
			}
			_, err := NewHandler(cnf, tc.supportedDeliveries)
			if tc.wantErrKind != "" {
				if err == nil {
					t.Fatalf("got no error; want error kind: %v", tc.wantErrKind)
				}
				err = errors.Cause(err)
				if v, ok := err.(*valError); !ok || v.kind != tc.wantErrKind {
					t.Fatalf("got error kind: %v; want error kind: %v", v.kind, tc.wantErrKind)
				}
				return
			}
			if err != nil {
				t.Fatalf("got error: %v; want no errors", err)
			}
		})
	}
}

func TestHandleSendMessage(t *testing.T) {
	testCases := []struct {
		name       string
		query      string
		body       string
		targets    string
		senders    map[DeliveryType]Sender
		wantBody   string
		wantMsg    Message
		wantStatus int
	}{
		{
			name:       "without target",
			body:       "Parameter 'target' is missed",
			wantBody:   "Parameter 'target' is missed",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "without body",
			targets:    "test:smtp:email@example.com",
			query:      "target=test",
			body:       "",
			wantBody:   "No body",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid body",
			targets:    "test:smtp:email@example.com",
			query:      "target=test",
			body:       "Invalid body",
			wantBody:   "Invalid body",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "without required field: text",
			targets:    "test:smtp:email@example.com",
			query:      "target=test",
			body:       `{"subject":"Test Subject","text":""}`,
			wantBody:   "Missing required fields: text",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "unknown target",
			targets:    "test:smtp:email@example.com",
			query:      "target=badtarget",
			body:       `{"subject":"Test Subject","text":"Test Message"}`,
			wantBody:   "Unknown target \"badtarget\"",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:    "sender error",
			targets: "test:smtp:email@example.com",
			query:   "target=test",
			senders: map[DeliveryType]Sender{DeliverySMTP: testNewSender(errors.New("Unknown error"))},
			body:    `{"subject":"Test Subject","text":"Test Message"}`,
			wantMsg: Message{
				Subject: "Test Subject",
				Text:    "Test Message",
			},
			wantStatus: http.StatusOK,
		},
		{
			name:    "all ok",
			targets: "test:smtp:email@example.com",
			query:   "target=test",
			senders: map[DeliveryType]Sender{DeliverySMTP: testNewSender(errors.New("Unknown error"))},
			body:    `{"subject":"Test Subject","text":"Test Message"}`,
			wantMsg: Message{
				Subject: "Test Subject",
				Text:    "Test Message",
			},
			wantStatus: http.StatusOK,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			url := "/"
			if tc.query != "" {
				url += "?" + tc.query
			}
			r, err := http.NewRequest(http.MethodPost, url, strings.NewReader(tc.body))
			if err != nil {
				t.Fatal(err)
			}

			rr := httptest.NewRecorder()
			tgtConf := TargetsConfig{}
			if err = tgtConf.Decode(tc.targets); err != nil {
				t.Fatalf("unexpected decode error: %s", err)
			}
			newMessageHandler(tgtConf, tc.senders).ServeHTTP(rr, r)

			if code := rr.Code; code != tc.wantStatus {
				t.Errorf("got status: %d; want status: %d", code, tc.wantStatus)
			}
			if body := strings.Trim(rr.Body.String(), "\n"); body != tc.wantBody {
				t.Errorf("got body: %s; want body: %s", body, tc.wantBody)
			}

			if rr.Code == 200 {
				for dlvName, v := range tc.senders {
					sender := v.(*testSender)
					sender.wg.Wait()
					if !sender.msgSent {
						t.Errorf("Sender of delivery %q is not called", dlvName)
					}
					if sender.msg != tc.wantMsg {
						t.Errorf("got message: %s; want message: %s", sender.msg, tc.wantMsg)
					}
				}
			}
		})
	}
}

type testSender struct {
	err     error
	msg     Message
	msgSent bool
	wg      sync.WaitGroup
}

func testNewSender(err error) *testSender {
	sender := &testSender{err: err}
	sender.wg.Add(1)
	return sender
}

func (s *testSender) Send(recipients []string, msg Message) error {
	defer s.wg.Done()
	s.msg = msg
	s.msgSent = true
	return s.err
}

func TestSMTPSender(t *testing.T) {
	var (
		errNet      = newTestNetError(false)
		errNetTemp1 = newTestNetError(true)
		errNetTemp2 = newTestNetError(true)
	)
	testCases := []struct {
		name        string
		errs        []error
		wantRetries int
		wantErr     error
	}{
		{
			name:        "all errors",
			errs:        []error{errNetTemp1, errNetTemp2, errNet},
			wantErr:     errNet,
			wantRetries: 3,
		},
		{
			name:        "partially failed",
			errs:        []error{errNetTemp1, errNetTemp2},
			wantRetries: 3,
		},
		{
			name:        "ok",
			wantRetries: 1,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var cnf SMTPConfig
			cnf.Retries = []time.Duration{0, 0, 0}
			sender := NewSMTPSender(cnf)

			var (
				cnt  int
				errs = append([]error{}, tc.errs...)
			)
			sender.sendfn = func(mail *mailyak.MailYak) error {
				cnt++
				if len(errs) == 0 {
					return nil
				}
				err := errs[0]
				errs = errs[1:]
				return err
			}

			err := sender.Send([]string{"email@example.com"}, Message{})

			if cnt != tc.wantRetries {
				t.Errorf("got retries: %d; want retries: %d", cnt, tc.wantRetries)
			}

			if tc.wantErr != nil {
				if err == nil {
					t.Fatalf("got no error; want error: %v", tc.wantErr)
				}
				if tc.wantErr != err {
					t.Fatalf("got error: %v; want error: %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("got error: %v; want no error", err)
			}
		})
	}
}

type testNetError struct {
	err    error
	isTemp bool
}

func newTestNetError(isTemp bool) *testNetError {
	return &testNetError{isTemp: isTemp}
}

func (e *testNetError) Error() string {
	return e.err.Error()
}

func (e *testNetError) Timeout() bool {
	return false
}

func (e *testNetError) Temporary() bool {
	return e.isTemp
}
