/*
Copyright (c) JSC iCore.

This source code is licensed under the MIT license found in the
LICENSE file in the root directory of this source tree.
*/

package notifr

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"

	"github.com/i-core/rlog"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// TargetsConfig is a configuration of routing messages to targets.
type TargetsConfig struct {
	targets map[string]*target
}

// target is a named group of delivery services.
type target struct {
	deliveries []*delivery
}

// delivery is a configuration for delivery service.
type delivery struct {
	name       DeliveryType
	recipients []string
}

// valError is an error that happens when parsing and validating target configuration.
type valError struct {
	kind   valErrKind
	target string
}

type valErrKind string

const (
	// An error that happens when a field in a target config is invalid.
	errKindInvTargetSyntax valErrKind = "invalid target's syntax"
	// An error that happens when a target config is not specified.
	errKindEmptyTargets valErrKind = "empty targets"
	// An error that happens when delivery in a target config is not supported.
	errKindUnsupportedDelivery valErrKind = "unsupported delivery type"
	// An error that happens when an email in a target config is invalid.
	errKindInvalidEmail valErrKind = "invalid email"
)

func (e *valError) Error() string {
	var sb strings.Builder
	sb.WriteString(string(e.kind))
	if e.target != "" {
		sb.WriteString(fmt.Sprintf(": %q", e.target))
	}
	return sb.String()
}

// Decode decodes a string in the format "target1:delivery1:recipient1,target2:delivery2:recipient2" to TargetsConfig.
func (cnf *TargetsConfig) Decode(value string) error {
	if value == "" {
		return nil
	}
	if cnf.targets == nil {
		cnf.targets = make(map[string]*target)
	}

	// Configuration of the targets is divided into a target, delivery, recipient for TargetConfig filling.
	for _, v := range strings.Split(value, ",") {
		elem := strings.Split(v, ":")
		if len(elem) != 3 {
			return &valError{kind: errKindInvTargetSyntax, target: v}
		}
		tgtName, dlvName, rcpt := elem[0], elem[1], elem[2]
		if tgtName == "" || dlvName == "" || rcpt == "" {
			return &valError{kind: errKindInvTargetSyntax, target: v}
		}
		tgt, ok := cnf.targets[tgtName]
		if !ok {
			tgt = &target{}
			cnf.targets[tgtName] = tgt
		}

		// Checking if the delivery type is supported.
		var dlv *delivery
		for _, delivery := range tgt.deliveries {
			if delivery.name == DeliveryType(dlvName) {
				dlv = delivery
				break
			}
		}
		if dlv == nil {
			dlv = &delivery{name: DeliveryType(dlvName)}
			tgt.deliveries = append(tgt.deliveries, dlv)
		}

		dlv.recipients = append(dlv.recipients, rcpt)
	}
	return nil
}

// MarshalJSON serializes TargetsConfig to a string in the format "target1:delivery1:recipient1,target2:delivery2:recipient2".
// It is needed for the correct output in logs.
func (cnf TargetsConfig) MarshalJSON() ([]byte, error) {
	var vv []string
	for targetName, target := range cnf.targets {
		for _, delivery := range target.deliveries {
			for _, recipient := range delivery.recipients {
				vv = append(vv, fmt.Sprintf("%s:%s:%s", targetName, delivery.name, recipient))
			}
		}
	}

	return []byte(fmt.Sprintf("%q", strings.Join(vv, ","))), nil
}

var reEmail = regexp.MustCompile("[a-z0-9!#$%&'*+/=?^_`{|}~-]+(?:\\.[a-z0-9!#$%&'*+/=?^_`{|}~-]+)*@(?:[a-z0-9](?:[a-z0-9-]*[a-z0-9])?\\.)+[a-z0-9](?:[a-z0-9-]*[a-z0-9])?")

// DeliveryType is a delivery type.
type DeliveryType string

// DeliverySMTP is an SMTP delivery type.
const DeliverySMTP DeliveryType = "smtp"

// Sender is an interface to send a message to a delivery service.
type Sender interface {
	Send(recipients []string, msg Message) error
}

// Handler is an HTTP handler that receives messages over HTTP and sends them to configured deliveries.
type Handler struct {
	senders map[DeliveryType]Sender
	targets TargetsConfig
}

// NewHandler returns a new instance of Handler.
func NewHandler(targets TargetsConfig, senders map[DeliveryType]Sender) (*Handler, error) {
	var supportedDeliveries []DeliveryType
	for v := range senders {
		supportedDeliveries = append(supportedDeliveries, v)
	}
	if err := validateTargetConfig(supportedDeliveries, targets); err != nil {
		return nil, errors.Wrap(err, "invalid target configuration")
	}
	return &Handler{senders: senders, targets: targets}, nil
}

// validateTargetConfig checks that TargetsConfig contains supported deliveries and valid recipients.
func validateTargetConfig(supportedDeliveries []DeliveryType, cnf TargetsConfig) error {
	if len(cnf.targets) == 0 {
		return &valError{kind: errKindEmptyTargets}
	}
	for targetName, target := range cnf.targets {
		for _, delivery := range target.deliveries {
			for _, recipient := range delivery.recipients {
				var deliverySupported bool
				// Validate that deliveries are supported.
				// We validate deliveries in the recipient's loop to format errors as `"target:delivery:recipient": cause`.
				// It is easier for a user to read errors in this format.
				for _, v := range supportedDeliveries {
					if delivery.name == v {
						deliverySupported = true
						break
					}
				}
				targetString := fmt.Sprintf("%s:%s:%s", targetName, delivery.name, recipient)
				if !deliverySupported {
					return &valError{kind: errKindUnsupportedDelivery, target: targetString}
				}
				if delivery.name == DeliverySMTP {
					if !reEmail.MatchString(recipient) {
						return &valError{kind: errKindInvalidEmail, target: targetString}
					}
				}
			}
		}
	}
	return nil
}

// AddRoutes registers all required routes for the package notifr.
func (srv *Handler) AddRoutes(apply func(m, p string, h http.Handler, mws ...func(http.Handler) http.Handler)) {
	apply(http.MethodPost, "", newMessageHandler(srv.targets, srv.senders))
}

// Message is a message received in an HTTP request for transferring to delivery service.
type Message struct {
	Subject string `json:"subject"`
	Text    string `json:"text"`
}

// newMessageHandler returns an HTTP handler that forwards a message to delivery services for a specified target.
// An HTTP request must contain a query parameter "target". A parameter's value is a target's name.
// An HTTP request must contain a body that is JSON object conforms struct "message".
func newMessageHandler(targetsConfig TargetsConfig, senders map[DeliveryType]Sender) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log := rlog.FromContext(r.Context()).Sugar()

		targetName := r.URL.Query().Get("target")
		if targetName == "" {
			msg := fmt.Sprintln("Parameter 'target' is missed")
			http.Error(w, msg, http.StatusBadRequest)
			log.Debug(msg)
			return
		}

		target, ok := targetsConfig.targets[targetName]
		if !ok {
			http.Error(w, fmt.Sprintf("Unknown target %q", targetName), http.StatusBadRequest)
			log.Debugf("Unknown target: %s", targetName)
			return
		}

		if r.Body == http.NoBody {
			msg := fmt.Sprintln("No body")
			http.Error(w, msg, http.StatusBadRequest)
			log.Debug(msg)
			return
		}

		var msg Message
		if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
			msg := fmt.Sprintln("Invalid body")
			http.Error(w, msg, http.StatusBadRequest)
			log.Debugf(msg, zap.Error(err))
			return
		}
		if msg.Text == "" {
			msg := fmt.Sprintln("Missing required fields: text")
			http.Error(w, msg, http.StatusBadRequest)
			log.Debug(msg)
			return
		}

		var wg sync.WaitGroup
		wg.Add(len(target.deliveries))
		for _, dlv := range target.deliveries {
			// We do not check the existence of the sender because the NewHandler function guarantees that a sender will exist for all types of delivery.
			sender := senders[dlv.name]
			go func(dlv *delivery, msg Message) {
				defer wg.Done()
				if err := sender.Send(dlv.recipients, msg); err != nil {
					log.Infow("Failed to send message", "delivery", dlv.name, zap.Error(err), "message", msg)
				}
			}(dlv, msg)
		}
		wg.Wait()
	}
}
