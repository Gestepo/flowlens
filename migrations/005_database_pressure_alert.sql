INSERT INTO alert_rules (id,kind,name,enabled,severity,config) VALUES
  ('database-pressure','database_pressure','数据库容量压力',true,'warning','{"threshold":80,"window_seconds":300}')
ON CONFLICT (id) DO NOTHING;
