
import io
from grafanalib.core import (
    Dashboard, Graph,
    OPS_FORMAT, Row,
    single_y_axis, Target, TimeRange, YAxes, YAxis
)
from grafanalib._gen import write_dashboard

    # origname="rtv.db.lockwait"
    # origname="rtv.db.ckp.active"
    # origname="rtv.db.ckp.records"
    # origname="rtv.db.io.records"
    # origname="rtv.rpl.behind.bytes"
    # origname="rtv.rpl.behind.journals"
    # origname="rtv.svr.sessions.active"
    # origname="rtv.svr.sessions.total"

dashboard = Dashboard(
    title="Python generated dashboard3",
    rows=[
        Row(panels=[
          Graph(
              title="p4_rtv_db_lockwait",
              dataSource='default',
              targets=[
                  Target(
                    expr='p4_rtv_db_lockwait',
                    legendFormat="instance {{instance}}, serverid {{serverid}}",
                    refId='A',
                  ),
              ],
              yAxes=single_y_axis(),
          ),
        ]),
        Row(panels=[
          Graph(
              title="p4_rtv_rpl_behind_bytes",
              dataSource='default',
              targets=[
                  Target(
                    expr='p4_rtv_rpl_behind_bytes',
                    legendFormat="instance {{instance}}, serverid {{serverid}}",
                    refId='A',
                  ),
              ],
              yAxes=single_y_axis(),
          ),
        ]),
        Row(panels=[
          Graph(
              title="p4_rtv_rpl_behind_bytes",
              dataSource='default',
              targets=[
                  Target(
                    expr='p4_rtv_rpl_behind_bytes',
                    legendFormat="instance {{instance}}, serverid {{serverid}}",
                    refId='A',
                  ),
              ],
              yAxes=single_y_axis(),
          ),
        ]),
    ],
).auto_panel_ids()

s = io.StringIO()
write_dashboard(dashboard, s)
print("""{
"dashboard": %s
}
""" % s.getvalue())
