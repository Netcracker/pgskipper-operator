import os

default_tags = {
    "namespace": os.environ['NAMESPACE'],
    "pod_name": os.environ['HOSTNAME'],
    "selector": "health",
    "service_name": "pg-common-collector",
}


class Metric:
    """
    This is a simple wrapper class for metrics
    """

    def __init__(self, metric_name, tags, fields):
        self.metric_name = metric_name
        self._tags = tags.copy()
        self._fields = fields.copy()

    @property
    def fields(self):
        return ",".join(
            ["{}={}".format(t, self._fields[t]) for t in self._fields])

    @property
    def tags(self):
        return ",".join(["{}={}".format(t, self._tags[t]) for t in self._tags])

    def get_field_value(self, field, default):
        return self._fields.get(field, default)


def convert_metrics(cluster_name, metrics):
    for metric in metrics:
        print(f"ma.pg.{cluster_name}.{metric.metric_name},{metric.tags} {metric.fields}")
