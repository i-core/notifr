/*
Copyright (c) JSC iCore.

This source code is licensed under the MIT license found in the
LICENSE file in the root directory of this source tree.
*/

package notifr

import (
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/domodwyer/mailyak"
	strip "github.com/grokify/html-strip-tags-go"
	blackfriday "github.com/russross/blackfriday/v2"
)

// SMTPConfig is configuration for SMTP Relay connection.
type SMTPConfig struct {
	Host    string          `envconfig:"host" required:"true" desc:"a host of an SMTP relay"`
	Port    int             `envconfig:"port" default:"587" desc:"a port of an SMTP relay"`
	From    string          `envconfig:"from" desc:"a sender email address"`
	Retries []time.Duration `envconfig:"retries" default:"10s,1m,10m" desc:"intervals to retry email sending"`
}

// SMTPSender is a message sender that sends a message by SMTP.
type SMTPSender struct {
	SMTPConfig
	sendfn func(*mailyak.MailYak) error
}

// NewSMTPSender returns a new SMTPSender.
func NewSMTPSender(cnf SMTPConfig) *SMTPSender {
	return &SMTPSender{
		SMTPConfig: cnf,
		sendfn:     func(mail *mailyak.MailYak) error { return mail.Send() },
	}
}

// Email header fields (including the Subject field) can be multi-line, with each line recommended to be no more than 78 characters.
// Header fields defined by RFC 5322 (https://tools.ietf.org/html/rfc5322#section-2.2).
// More details about line length limits in the RFC 2822 (https://tools.ietf.org/html/rfc2822#section-2.1.1).
const subjectMaxLen = 78

// Send sends a message by SMTP.
// The method tries to re-send a message when the previous sending failed with a temporary network error.
func (s *SMTPSender) Send(recipients []string, msg Message) error {
	// These actions allow to correctly display the tables in the received emails, otherwise, without using CSS, the table frames are not displayed.
	css := `<style>table,th,td{border: 1px solid black;} tr:nth-child(even){background-color: grey;}</style>`
	md := string(blackfriday.Run([]byte(msg.Text)))
	html := `
<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.0 Transitional//EN" "http://www.w3.org/TR/xhtml1/DTD/xhtml1-transitional.dtd">
<html xmlns="http://www.w3.org/1999/xhtml" xmlns:o="urn:schemas-microsoft-com:office:office">
	<head>
		<title>Message</title>` +
		css +
		`</head>
	<body>` +
		md +
		`</body>
</html>
`

	mail := mailyak.New(fmt.Sprintf("%s:%d", s.Host, s.Port), nil)

	mail.To(recipients...)
	if s.From != "" {
		mail.From(s.From)
	}
	subject := msg.Subject
	if subject == "" {
		plainText := strip.StripTags(md)
		for _, line := range strings.Split(plainText, "\n") {
			if line != "" {
				subject = line
				break
			}
		}
		if len(subject) > subjectMaxLen {
			subject = subject[:subjectMaxLen]
		}
	}
	mail.Subject(subject)
	mail.Plain().Set(msg.Text)
	mail.HTML().Set(html)

	var err error
	for _, n := range s.Retries {
		if err = s.sendfn(mail); err == nil {
			break
		}
		if v, ok := err.(net.Error); !(ok && v.Temporary()) {
			return err
		}
		time.Sleep(n)
	}
	if err != nil {
		return err
	}
	return nil
}
