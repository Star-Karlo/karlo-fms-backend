package platformadmin

import (
	"log"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/platform/catalog"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
	"github.com/karlo/fms-backend/internal/platform/mdclient"
	"github.com/karlo/fms-backend/internal/platform/mdwrite"
)

// One-off: FMS's fleet groups and memberships into master data.
//
// Master data owns vehicle groups now, but it starts with none for the
// ~2,000 trucks that are grouped here. Projecting membership before this
// ran would ungroup the fleet, which is why GROUPSYNC_MEMBERSHIP is off
// until it has. As the operator, acting for each account: every unpaired
// fleet group is created upstream (or adopted by name when master data
// already has one), then every paired vehicle's group is set upstream.

// GroupsBackfillResult says what the backfill did.
type GroupsBackfillResult struct {
	Accounts       int `json:"accounts"`
	GroupsAdopted  int `json:"groups_adopted"`
	GroupsCreated  int `json:"groups_created"`
	GroupsFailed   int `json:"groups_failed"`
	VehiclesSet    int `json:"vehicles_set"`
	VehiclesFailed int `json:"vehicles_failed"`
	// VehiclesUnpaired: grouped here but with no master_data_id — nothing to
	// set upstream until vehiclesync pairs them.
	VehiclesUnpaired int `json:"vehicles_unpaired"`
}

// WithMasterDataClient supplies the gRPC read the backfill adopts groups by.
func (h *Handler) WithMasterDataClient(md *mdclient.Client) *Handler { h.mdRead = md; return h }

// groupsBackfill godoc
//
//	@Summary		Push FMS fleet groups and memberships to master data (platform admin)
//	@Description	For every account, as the operator: create each fleet group master data lacks (adopting one with the same name if it exists) and record it as paired; then set truckGroupId on every paired vehicle from its FMS fleet group. Run once before switching GROUPSYNC_MEMBERSHIP on; idempotent.
//	@Tags			platform
//	@Security		BearerAuth
//	@Produce		json
//	@Success		200	{object}	platformadmin.GroupsBackfillResult
//	@Failure		503	{object}	map[string]string	"master data not configured"
//	@Router			/v1/platform/groups-backfill [post]
func (h *Handler) groupsBackfill(w http.ResponseWriter, r *http.Request) {
	if h.md == nil || h.mdRead == nil || h.dir == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "master data is not configured")
		return
	}
	tenants, err := h.tenantList(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "list accounts failed")
		return
	}
	bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	var res GroupsBackfillResult
	for _, t := range tenants {
		company, _ := h.dir.CompanyForTenant(r.Context(), t.id)
		if company == "" {
			continue
		}
		res.Accounts++
		caller := mdwrite.Caller{Bearer: bearer, ActingFor: company}

		// Master data's groups, by normalised name, for adoption.
		upstream := map[string]string{}
		if gs, err := h.mdRead.ListTruckGroups(r.Context(), company); err == nil {
			for _, g := range gs {
				if !g.Deleted {
					upstream[catalog.Normalise(g.Name)] = g.ID
				}
			}
		} else {
			log.Printf("groups backfill: %s: list upstream groups: %v", t.name, err)
		}

		// 1. Groups: pair every unpaired local group.
		type grp struct {
			id   int64
			name string
			desc string
		}
		var unpaired []grp
		_ = db.Tx(r.Context(), h.pool, t.id, func(tx pgx.Tx) error {
			rows, err := tx.Query(r.Context(), `SELECT id, name, coalesce(description,'') FROM fleet_groups WHERE master_data_id IS NULL ORDER BY id`)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var g grp
				if err := rows.Scan(&g.id, &g.name, &g.desc); err != nil {
					return err
				}
				unpaired = append(unpaired, g)
			}
			return rows.Err()
		})
		for _, g := range unpaired {
			mdID, ok := upstream[catalog.Normalise(g.name)]
			if ok {
				res.GroupsAdopted++
			} else {
				id, err := h.md.CreateVehicleGroup(r.Context(), caller, mdwrite.VehicleGroup{Name: g.name, Description: g.desc})
				if err != nil || id == "" {
					res.GroupsFailed++
					log.Printf("groups backfill: %s group %q: %v", t.name, g.name, err)
					continue
				}
				mdID = id
				res.GroupsCreated++
			}
			_ = db.Tx(r.Context(), h.pool, t.id, func(tx pgx.Tx) error {
				_, err := tx.Exec(r.Context(), `UPDATE fleet_groups SET master_data_id = $2, updated_at = now() WHERE id = $1 AND master_data_id IS NULL`, g.id, mdID)
				return err
			})
		}

		// 2. Membership: every paired vehicle with a (now paired) group.
		type mem struct {
			vehicleMD string
			groupMD   *string
		}
		var mems []mem
		_ = db.Tx(r.Context(), h.pool, t.id, func(tx pgx.Tx) error {
			rows, err := tx.Query(r.Context(), `
				SELECT v.master_data_id, g.master_data_id
				  FROM vehicles v JOIN fleet_groups g ON g.id = v.fleet_group_id
				 WHERE v.status <> 'archive'`)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var vmd, gmd *string
				if err := rows.Scan(&vmd, &gmd); err != nil {
					return err
				}
				if vmd == nil {
					res.VehiclesUnpaired++
					continue
				}
				mems = append(mems, mem{vehicleMD: *vmd, groupMD: gmd})
			}
			return rows.Err()
		})
		for _, m := range mems {
			if m.groupMD == nil {
				res.VehiclesFailed++ // its group could not be paired above
				continue
			}
			if err := h.md.SetVehicleGroup(r.Context(), caller, m.vehicleMD, m.groupMD); err != nil {
				res.VehiclesFailed++
				log.Printf("groups backfill: %s vehicle %s: %v", t.name, m.vehicleMD, err)
				continue
			}
			res.VehiclesSet++
		}
	}
	log.Printf("groups backfill by user %d: %+v", auth.UserID(r.Context()), res)
	httpx.JSON(w, http.StatusOK, res)
}
