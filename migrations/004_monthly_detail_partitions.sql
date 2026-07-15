DO $partition_migration$
DECLARE
  parent_name text;
  default_name text;
  first_observation timestamptz;
  last_observation timestamptz;
  month_start timestamptz;
  final_month timestamptz;
  partition_name text;
BEGIN
  FOREACH parent_name IN ARRAY ARRAY['interface_deltas', 'connection_details', 'proxy_request_details'] LOOP
    default_name := parent_name || '_default';
    EXECUTE format('LOCK TABLE %I IN ACCESS EXCLUSIVE MODE', parent_name);
    EXECUTE format('ALTER TABLE %I DETACH PARTITION %I', parent_name, default_name);
    EXECUTE format('SELECT min(observed_at), max(observed_at) FROM %I', default_name)
      INTO first_observation, last_observation;

    month_start := date_trunc('month', least(coalesce(first_observation, now() - interval '2 months'), now() - interval '2 months'));
    final_month := date_trunc('month', greatest(coalesce(last_observation, now() + interval '2 months'), now() + interval '2 months'));
    WHILE month_start <= final_month LOOP
      partition_name := parent_name || '_' || to_char(month_start, 'YYYYMM');
      EXECUTE format(
        'CREATE TABLE IF NOT EXISTS %I PARTITION OF %I FOR VALUES FROM (%L) TO (%L)',
        partition_name, parent_name, month_start, month_start + interval '1 month'
      );
      month_start := month_start + interval '1 month';
    END LOOP;

    EXECUTE format('INSERT INTO %I SELECT * FROM %I', parent_name, default_name);
    EXECUTE format('DROP TABLE %I', default_name);
    EXECUTE format('CREATE TABLE %I PARTITION OF %I DEFAULT', default_name, parent_name);
  END LOOP;
END
$partition_migration$;
