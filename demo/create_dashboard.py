
import io
from grafanalib.core import (
    Dashboard, Graph,
    OPS_FORMAT, Row,
    single_y_axis, Target, TimeRange, YAxes, YAxis
)
from grafanalib._gen import write_dashboard

metrics = """
p4_rtv_db_lockwait
p4_rtv_db_ckp_active
p4_rtv_db_ckp_records
p4_rtv_db_io_records
p4_rtv_rpl_behind_bytes
p4_rtv_rpl_behind_journals
p4_rtv_svr_sessions_active
p4_rtv_svr_sessions_total
p4_locks_db_read
p4_locks_db_write
p4_locks_db_read_by_table
p4_locks_db_write_by_table
p4_locks_cliententity_read
p4_locks_cliententity_write
p4_locks_meta_read
p4_locks_meta_write
p4_locks_cmds_blocked
p4_locks_cmds_blocking_by_cmd
""".split("\n")

dashboard = Dashboard(
    title="Python generated dashboard"
)

for metric in metrics:
    if not metric.strip():
        continue
    graph = Graph(title=metric,
                    dataSource='default',
                    targets=[Target(
                            expr=metric,
                            legendFormat="instance {{instance}}, serverid {{serverid}}",
                            refId='A',
                        ),
                    ],
                    yAxes=single_y_axis(),
                )
    dashboard.rows.append(
        Row(panels=[graph])
    )

# Auto-number panels - returns new dashboard
dashboard = dashboard.auto_panel_ids()

s = io.StringIO()
write_dashboard(dashboard, s)
print("""{
"dashboard": %s
}
""" % s.getvalue())
