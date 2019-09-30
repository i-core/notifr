/*
Copyright (c) JSC iCore.

This source code is licensed under the MIT license found in the
LICENSE file in the root directory of this source tree.
*/

package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"

	"github.com/i-core/notifr/internal/notifr"
	"github.com/i-core/notifr/internal/stat"
	"github.com/i-core/rlog"
	"github.com/i-core/routegroup"
	"github.com/kelseyhightower/envconfig"
	"go.uber.org/zap"
)

// version will be filled at compile time.
var version = ""

type config struct {
	DevMode bool                 `envconfig:"dev_mode" default:"false" desc:"a development mode"`
	Listen  string               `envconfig:"listen" default:":8080" desc:"a host and port to listen on (<host>:<port>)"`
	Targets notifr.TargetsConfig `envconfig:"targets" required:"true" desc:"configuration for routing messages by target name (<target>:<delivery>:<recipient>)"`
	SMTP    notifr.SMTPConfig
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage of %s:\n", os.Args[0])
		flag.PrintDefaults()
		fmt.Fprintf(flag.CommandLine.Output(), "\n")
		if err := envconfig.Usagef("notifr", &config{}, flag.CommandLine.Output(), envconfig.DefaultListFormat); err != nil {
			panic(err)
		}
	}
	verFlag := flag.Bool("version", false, "print a version")
	flag.Parse()
	if *verFlag {
		fmt.Println("notifr", version)
		os.Exit(0)
	}

	var cnf config
	if err := envconfig.Process("notifr", &cnf); err != nil {
		fmt.Fprintf(os.Stderr, "Invalid configuration: %s\n", err)
		os.Exit(1)
	}

	logFunc := zap.NewProduction
	if cnf.DevMode {
		logFunc = zap.NewDevelopment
	}
	log, err := logFunc()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create the logger: %s\n", err)
		os.Exit(1)
	}

	senders := map[notifr.DeliveryType]notifr.Sender{
		notifr.DeliverySMTP: notifr.NewSMTPSender(cnf.SMTP),
	}

	router := routegroup.NewRouter(rlog.NewMiddleware(log))
	handler, err := notifr.NewHandler(cnf.Targets, senders)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create the notification handler: %s\n", err)
		os.Exit(1)
	}
	router.AddRoutes(handler, "/notifr")
	router.AddRoutes(stat.NewHandler(version), "/stat")

	log = log.Named("main")
	log.Info("notifr started", zap.Any("config", cnf), zap.String("version", version))
	log.Fatal("notifr finished", zap.Error(http.ListenAndServe(cnf.Listen, router)))
}
