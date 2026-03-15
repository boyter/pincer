package main

import (
	"context"
	"github.com/boyter/pincer/common"
	"github.com/boyter/pincer/handlers"
	"github.com/boyter/pincer/service"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
)

func main() {
	environment := common.NewEnvironment()
	zerolog.SetGlobalLevel(zerolog.InfoLevel)

	ser, err := service.NewService(environment)
	if err != nil {
		log.Error().Str(common.UniqueCode, "715c6344").Err(err).Msg("error creating service")
		return
	}

	app, err := handlers.NewApplication(environment, ser)
	if err != nil {
		log.Error().Str(common.UniqueCode, "b3b46e8b").Err(err).Msg("error creating application")
		return
	}

	ser.BootstrapPreviewData()

	app.StartBackgroundJobs() // IP cleanup job
	ser.StartBackgroundJobs() // Run background jobs now as we are in real mode
	srv := &http.Server{
		Addr:    ":" + strconv.Itoa(environment.HttpPort),
		Handler: app.Routes(),
	}

	// Graceful shutdown: save data on SIGINT/SIGTERM (systemd stop, ctrl-c)
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-quit
		log.Info().Str(common.UniqueCode, "a7e3f1b2").Str("signal", sig.String()).Msg("shutdown signal received, saving data")
		ser.SaveActivity()
		ser.SaveBots()
		log.Info().Str(common.UniqueCode, "c4d8e2a1").Msg("data saved, shutting down server")
		srv.Shutdown(context.Background())
	}()

	log.Log().Str(common.UniqueCode, "3812c7e").Msg("starting server on :" + strconv.Itoa(environment.HttpPort))
	err = srv.ListenAndServe()
	if err != nil && err != http.ErrServerClosed {
		log.Error().Str(common.UniqueCode, "42aa9c1").Err(err).Msg("exiting server")
	}
}
