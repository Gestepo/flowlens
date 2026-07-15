package rollup

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	ResolutionHour = "hour"
	ResolutionDay  = "day"
)

type Service struct{ pool *pgxpool.Pool }

func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

func (service *Service) RollupRange(ctx context.Context, resolution string, start, end time.Time) error {
	if resolution != ResolutionHour && resolution != ResolutionDay {
		return errors.New("rollup resolution must be hour or day")
	}
	start, end = start.UTC(), end.UTC()
	if !end.After(start) {
		return errors.New("rollup end must be after start")
	}
	tx, err := service.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM traffic_rollups WHERE resolution=$1 AND bucket >= $2 AND bucket < $3`, resolution, start, end); err != nil {
		return err
	}
	statements := []string{
		`INSERT INTO traffic_rollups(resolution,node_id,bucket,dimension_kind,dimension_key,direction,status_code,bytes,packets,connections,requests,source_min_at,source_max_at)
		 SELECT $1,node_id,date_trunc($1,bucket,'UTC'),'traffic','total',direction,0,sum(bytes),sum(packets),0,0,min(bucket),max(bucket)
		 FROM traffic_minute WHERE bucket >= $2 AND bucket < $3 GROUP BY node_id,date_trunc($1,bucket,'UTC'),direction`,
		`INSERT INTO traffic_rollups(resolution,node_id,bucket,dimension_kind,dimension_key,direction,status_code,bytes,packets,connections,requests,source_min_at,source_max_at)
		 SELECT $1,node_id,date_trunc($1,bucket,'UTC'),'owner',jsonb_build_object('id',owner_id,'kind',owner_kind,'name',owner_name)::text,direction,0,sum(bytes),0,sum(connections),0,min(bucket),max(bucket)
		 FROM owner_minute WHERE bucket >= $2 AND bucket < $3 GROUP BY node_id,date_trunc($1,bucket,'UTC'),owner_id,owner_kind,owner_name,direction`,
		`INSERT INTO traffic_rollups(resolution,node_id,bucket,dimension_kind,dimension_key,direction,status_code,bytes,packets,connections,requests,source_min_at,source_max_at)
		 SELECT $1,node_id,date_trunc($1,bucket,'UTC'),'domain',jsonb_build_object('domain',domain,'confidence',confidence)::text,direction,0,sum(bytes),0,sum(connections),sum(requests),min(bucket),max(bucket)
		 FROM domain_minute WHERE bucket >= $2 AND bucket < $3 GROUP BY node_id,date_trunc($1,bucket,'UTC'),domain,confidence,direction`,
		`INSERT INTO traffic_rollups(resolution,node_id,bucket,dimension_kind,dimension_key,direction,status_code,bytes,packets,connections,requests,source_min_at,source_max_at)
		 SELECT $1,node_id,date_trunc($1,bucket,'UTC'),'flow',jsonb_build_object('owner_id',owner_id,'owner_name',owner_name,'source',source,'destination',destination,'domain',domain,'confidence',confidence,'protocol',protocol,'port',remote_port)::text,direction,0,sum(bytes),0,sum(connections),sum(requests),min(bucket),max(bucket)
		 FROM flow_minute WHERE bucket >= $2 AND bucket < $3 GROUP BY node_id,date_trunc($1,bucket,'UTC'),owner_id,owner_name,source,destination,domain,confidence,protocol,remote_port,direction`,
		`INSERT INTO traffic_rollups(resolution,node_id,bucket,dimension_kind,dimension_key,direction,status_code,bytes,packets,connections,requests,source_min_at,source_max_at)
		 SELECT $1,node_id,date_trunc($1,bucket,'UTC'),'proxy_status',host,'inbound',status,sum(bytes),0,0,sum(requests),min(bucket),max(bucket)
		 FROM proxy_status_minute WHERE bucket >= $2 AND bucket < $3 GROUP BY node_id,date_trunc($1,bucket,'UTC'),host,status`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(ctx, statement, resolution, start, end); err != nil {
			return fmt.Errorf("build %s rollup: %w", resolution, err)
		}
	}
	return tx.Commit(ctx)
}
