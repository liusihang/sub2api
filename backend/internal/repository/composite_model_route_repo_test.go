package repository

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"
)

func newCompositeModelRouteRepoSQLite(t *testing.T) (*compositeModelRouteRepository, *dbent.Client) {
	t.Helper()

	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?mode=memory&cache=shared&_fk=1", t.Name()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	drv := entsql.OpenDB(dialect.SQLite, db)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(drv)))
	t.Cleanup(func() { _ = client.Close() })

	return &compositeModelRouteRepository{client: client}, client
}

func TestCompositeModelRouteRepositoryUpdateClearsBillingRateMultiplier(t *testing.T) {
	repo, client := newCompositeModelRouteRepoSQLite(t)
	ctx := context.Background()

	group, err := client.Group.Create().
		SetName("composite-route-update-clear-rate").
		SetPlatform(service.PlatformComposite).
		Save(ctx)
	require.NoError(t, err)

	initialRate := 0.4
	created, err := client.CompositeModelRoute.Create().
		SetGroupID(group.ID).
		SetPublicModel("gpt-5").
		SetTargetPlatform(service.PlatformOpenAI).
		SetBillingRateMultiplier(initialRate).
		Save(ctx)
	require.NoError(t, err)

	route := compositeModelRouteEntityToService(created)
	route.BillingRateMultiplier = nil
	require.NoError(t, repo.Update(ctx, route))

	stored, err := client.CompositeModelRoute.Get(ctx, created.ID)
	require.NoError(t, err)
	require.Nil(t, stored.BillingRateMultiplier)
}
