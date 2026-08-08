package dal

import (
	"testing"

	"finance-sys/internal/domain/db_model"

	"github.com/stretchr/testify/require"
)

func TestSecurityMasterWriteRowPreservesFalseIsActive(t *testing.T) {
	row := securityMasterWriteRow(&db_model.SecurityMaster{
		TSCode:   "000001.SZ",
		IsActive: false,
	})

	require.Contains(t, row, "is_active")
	require.Equal(t, false, row["is_active"])
}
