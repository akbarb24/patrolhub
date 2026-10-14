package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/akbarbinaji/patrolhub/internal/config"
	"github.com/akbarbinaji/patrolhub/internal/domain"
	"github.com/akbarbinaji/patrolhub/internal/service"
)

type demoPerson struct{ block, name string }

// Demo team per ISO weekday (1 = Monday). Some people have only a block, like in the real sheet.
var demoTeams = [7][]demoPerson{
	{{"A-2", "Dhani"}, {"B-3", "Yusuf"}, {"A-5", "Adytya"}, {"B-4", "Ugi"}},
	{{"A-7", "Gemilang"}, {"B-1", "Wasis"}, {"C-10", ""}, {"E-10", ""}},
	{{"A-1", "Anton"}, {"C-4", "Joshua"}, {"D-2", "Robert"}, {"E-11", ""}},
	{{"F-1", "Pak Eres"}, {"D-6", "Bagas"}, {"C-8", "Rendi"}, {"H-12", ""}},
	{{"B-9", "Fajar"}, {"D-3", "Hendra"}, {"A-9", "Irfan"}, {"G-5", ""}},
	{{"E-4", "Kevin"}, {"F-6", "Lukman"}, {"G-2", "Maman"}, {"J-3", ""}},
	{{"C-12", "Nanda"}, {"E-7", "Oki"}, {"H-5", "Putra"}, {"K-1", ""}},
}

// seedDemo fills an empty database with sample members and weekly schedules.
func seedDemo(cfg config.Config) error {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	members, schedule := service.NewMemberService(pool), service.NewScheduleService(pool)
	existing, err := members.List(ctx)
	if err != nil {
		return err
	}
	if len(existing) > 0 {
		return errors.New("seed-demo: database already has members; refusing to add demo data")
	}

	from := domain.DateOf(time.Now()).AddDays(-60)
	count := 0
	for i, team := range demoTeams {
		for _, p := range team {
			m, err := members.Create(ctx, service.MemberInput{HouseBlock: p.block, Name: p.name})
			if err != nil {
				return err
			}
			if err := schedule.AddRule(ctx, m.ID, i+1, from); err != nil {
				return err
			}
			count++
		}
	}
	fmt.Fprintf(os.Stderr, "Data contoh dibuat: %d peserta, jadwal berlaku sejak %s.\n", count, from)
	return nil
}
