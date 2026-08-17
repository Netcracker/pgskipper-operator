package upgrade

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	pgClient "github.com/Netcracker/pgskipper-operator/pkg/client"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

var (
	replicationSlotsLagTimeout    = 30 * time.Minute
	upgradeReplicationDatabasesCM = "databases"
	replicationSlotsQuery         = `SELECT slot_name, slot_type, COALESCE(database, ''), active,
										CASE WHEN slot_type = 'logical'
     									THEN COALESCE(pg_wal_lsn_diff(pg_current_wal_lsn(), confirmed_flush_lsn), 0)
     									ELSE COALESCE(pg_wal_lsn_diff(pg_current_wal_lsn(), restart_lsn), 0)
										END AS lag
										FROM pg_replication_slots`

	terminateQuery = `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '%s' AND pid <> pg_backend_pid()`
	restoreROQuery = `ALTER DATABASE "%s" SET default_transaction_read_only = off`
	setRoQuery     = `ALTER DATABASE "%s" SET default_transaction_read_only = on`
)

type replicationSlot struct {
	Name     string
	SlotType string
	Database string
	Active   bool
	Lag      int64
}

func (u *Upgrade) getReplicationSlots(pgC *pgClient.PostgresClient) ([]replicationSlot, error) {
	conn, err := pgC.GetConnection()
	if err != nil {
		return nil, err
	}
	defer conn.Release()

	rows, err := conn.Query(context.Background(), replicationSlotsQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to query replication slots: %w", err)
	}
	defer rows.Close()

	slots := make([]replicationSlot, 0)
	for rows.Next() {
		var slot replicationSlot
		if err := rows.Scan(&slot.Name, &slot.SlotType, &slot.Database, &slot.Active, &slot.Lag); err != nil {
			return nil, fmt.Errorf("failed to scan replication slot: %w", err)
		}
		slots = append(slots, slot)
	}
	return slots, nil
}

func uniqueLogicalDatabases(slots []replicationSlot) []string {
	seen := make(map[string]struct{})
	databases := make([]string, 0)
	for _, slot := range slots {
		if slot.SlotType != "logical" || slot.Database == "" {
			continue
		}
		if _, exists := seen[slot.Database]; exists {
			continue
		}
		seen[slot.Database] = struct{}{}
		databases = append(databases, slot.Database)
	}
	return databases
}

func (u *Upgrade) prepareRODatabases(pgC *pgClient.PostgresClient, databases []string) error {
	for _, db := range databases {
		quotedDB := strings.ReplaceAll(db, `"`, `""`)
		if err := pgC.Execute(fmt.Sprintf(setRoQuery, quotedDB)); err != nil {
			return fmt.Errorf("failed to set database %q read-only: %w", db, err)
		}
		if err := pgC.Execute(fmt.Sprintf(terminateQuery, db)); err != nil {
			return fmt.Errorf("failed to terminate connections to database %q: %w", db, err)
		}
		logger.Info(fmt.Sprintf(`Database "%s" set to read-only and connections terminated`, db))
	}
	return nil
}

func replicationSlotDatabasesCMName(clusterName string) string {
	return fmt.Sprintf("%s-upgrade-replication-databases", clusterName)
}

func (u *Upgrade) saveRODatabasesCM(clusterName string, databases []string) error {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      replicationSlotDatabasesCMName(clusterName),
			Namespace: namespace,
		},
		Data: map[string]string{
			upgradeReplicationDatabasesCM: strings.Join(databases, ","),
		},
	}
	if _, err := u.helper.CreateOrUpdateConfigMap(cm); err != nil {
		return fmt.Errorf("failed to save replication slot databases configmap: %w", err)
	}
	return nil
}

func (u *Upgrade) waitForReplicationSlotsLagZero(pgC *pgClient.PostgresClient, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(context.Background(), 10*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		slots, err := u.getReplicationSlots(pgC)
		if err != nil {
			return false, err
		}
		for _, slot := range slots {
			if slot.Lag > 0 {
				logger.Info(fmt.Sprintf(`Replication slot "%s" still has lag %d bytes`, slot.Name, slot.Lag))
				return false, nil
			}
		}
		logger.Info("All replication slots have zero lag")
		return true, nil
	})
}

func (u *Upgrade) HandleReplicationSlotsBeforeUpgrade(pgHost, clusterName string) error {
	pgC := pgClient.GetPostgresClient(pgHost)
	slots, err := u.getReplicationSlots(pgC)
	if err != nil {
		return err
	}
	if len(slots) == 0 {
		logger.Info("No replication slots found. Safe to proceed with upgrade.")
		return nil
	}

	for _, slot := range slots {
		if !slot.Active && slot.Lag > 0 {
			return fmt.Errorf(
				"upgrade error: replication slot %q is not active and has lag %d bytes; cannot upgrade",
				slot.Name, slot.Lag,
			)
		}
	}

	databases := uniqueLogicalDatabases(slots)
	if len(databases) > 0 {
		if err := u.prepareRODatabases(pgC, databases); err != nil {
			return err
		}
		if err := u.saveRODatabasesCM(clusterName, databases); err != nil {
			return err
		}
	}

	if err := u.waitForReplicationSlotsLagZero(pgC, replicationSlotsLagTimeout); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("upgrade error: replication slots did not reach zero lag within %s", replicationSlotsLagTimeout)
		}
		return fmt.Errorf("upgrade error: failed waiting for replication slots lag to reach zero: %w", err)
	}

	logger.Info("Replication slots check passed. Safe to proceed with upgrade.")
	return nil
}

func (u *Upgrade) restoreRODatabases(pgC *pgClient.PostgresClient, databases []string) error {
	for _, db := range databases {
		if db == "" {
			continue
		}
		quotedDB := strings.ReplaceAll(db, `"`, `""`)
		if err := pgC.Execute(fmt.Sprintf(restoreROQuery, quotedDB)); err != nil {
			return fmt.Errorf("failed to unset read-only for database %q: %w", db, err)
		}
		logger.Info(fmt.Sprintf(`Database "%s" read-only mode disabled`, db))
	}
	return nil
}

func (u *Upgrade) RestoreRODatabases(pgHost, clusterName string) error {
	cm, err := u.helper.GetConfigMap(replicationSlotDatabasesCMName(clusterName))
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to get replication slot databases configmap: %w", err)
	}

	databases := strings.Split(cm.Data[upgradeReplicationDatabasesCM], ",")
	if len(databases) > 0 && databases[0] != "" {
		pgC := pgClient.GetPostgresClient(pgHost)
		if err := u.restoreRODatabases(pgC, databases); err != nil {
			return err
		}
	}

	if err := u.client.Delete(context.TODO(), cm); err != nil && !k8serrors.IsNotFound(err) {
		return fmt.Errorf("failed to delete replication slot databases configmap: %w", err)
	}
	logger.Info(fmt.Sprintf(`Replication slot databases configmap "%s" removed`, cm.Name))
	return nil
}
