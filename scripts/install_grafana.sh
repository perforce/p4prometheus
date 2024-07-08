#!/bin/sh

set -e

[ -f $BASE_DIR/etc/sysconfig/grafana-server ] && . $BASE_DIR/etc/sysconfig/grafana-server

BASE_DIR=/home/perforce.db/monitoring

startGrafana() {
  if [ -x /bin/systemctl ] ; then
    /bin/systemctl daemon-reload
    /bin/systemctl start grafana-server.service
  elif [ -x $BASE_DIR/etc/init.d/grafana-server ] ; then
    $BASE_DIR/etc/init.d/grafana-server start
  elif [ -x $BASE_DIR/etc/rc.d/init.d/grafana-server ] ; then
    $BASE_DIR/etc/rc.d/init.d/grafana-server start
  fi
}

stopGrafana() {
  if [ -x /bin/systemctl ] ; then
    /bin/systemctl stop grafana-server.service > /dev/null 2>&1 || :
  elif [ -x $BASE_DIR/etc/init.d/grafana-service ] ; then
    $BASE_DIR/etc/init.d/grafana-service stop
  elif [ -x $BASE_DIR/etc/rc.d/init.d/grafana-service ] ; then
    $BASE_DIR/etc/rc.d/init.d/grafana-service stop
  fi
}


# Initial installation: $1 == 1
# Upgrade: $1 == 2, and configured to restart on upgrade
if [ $1 -eq 1 ] ; then
  [ -z "$GRAFANA_USER" ] && GRAFANA_USER="grafana"
  [ -z "$GRAFANA_GROUP" ] && GRAFANA_GROUP="grafana"
  if ! getent group "$GRAFANA_GROUP" > /dev/null 2>&1 ; then
    groupadd -r "$GRAFANA_GROUP"
  fi
  if ! getent passwd "$GRAFANA_USER" > /dev/null 2>&1 ; then
    useradd -r -g "$GRAFANA_GROUP" -d $BASE_DIR/usr/share/grafana -s /sbin/nologin \
      -c "grafana user" "$GRAFANA_USER"
  fi

  # Set user permissions on /var/log/grafana, /var/lib/grafana
  mkdir -p /var/log/grafana $BASE_DIR/var/lib/grafana
  chown -R $GRAFANA_USER:$GRAFANA_GROUP /var/log/grafana $BASE_DIR/var/lib/grafana
  chmod 755 /var/log/grafana $BASE_DIR/var/lib/grafana

  # copy user config files
  if [ ! -f $CONF_FILE ]; then
    cp $BASE_DIR/usr/share/grafana/conf/sample.ini $CONF_FILE
    cp $BASE_DIR/usr/share/grafana/conf/ldap.toml $BASE_DIR/etc/grafana/ldap.toml
  fi

  if [ ! -d $PROVISIONING_CFG_DIR ]; then
    mkdir -p $PROVISIONING_CFG_DIR/dashboards $PROVISIONING_CFG_DIR/datasources
    cp $BASE_DIR/usr/share/grafana/conf/provisioning/dashboards/sample.yaml $PROVISIONING_CFG_DIR/dashboards/sample.yaml
    cp $BASE_DIR/usr/share/grafana/conf/provisioning/datasources/sample.yaml $PROVISIONING_CFG_DIR/datasources/sample.yaml
  fi

  if [ ! -d $PROVISIONING_CFG_DIR/notifiers ]; then
    mkdir -p $PROVISIONING_CFG_DIR/notifiers
    cp $BASE_DIR/usr/share/grafana/conf/provisioning/notifiers/sample.yaml $PROVISIONING_CFG_DIR/notifiers/sample.yaml
  fi

  if [ ! -d $PROVISIONING_CFG_DIR/plugins ]; then
    mkdir -p $PROVISIONING_CFG_DIR/plugins
    cp $BASE_DIR/usr/share/grafana/conf/provisioning/plugins/sample.yaml $PROVISIONING_CFG_DIR/plugins/sample.yaml
  fi

  if [ ! -d $PROVISIONING_CFG_DIR/access-control ]; then
    mkdir -p $PROVISIONING_CFG_DIR/access-control
    cp $BASE_DIR/usr/share/grafana/conf/provisioning/access-control/sample.yaml $PROVISIONING_CFG_DIR/access-control/sample.yaml
  fi

  if [ ! -d $PROVISIONING_CFG_DIR/alerting ]; then
    mkdir -p $PROVISIONING_CFG_DIR/alerting
    cp $BASE_DIR/usr/share/grafana/conf/provisioning/alerting/sample.yaml $PROVISIONING_CFG_DIR/alerting/sample.yaml
  fi

	# configuration files should not be modifiable by grafana user, as this can be a security issue
	chown -Rh root:$GRAFANA_GROUP $BASE_DIR/etc/grafana/*
	chmod 755 $BASE_DIR/etc/grafana
	find $BASE_DIR/etc/grafana -type f -exec chmod 640 {} ';'
	find $BASE_DIR/etc/grafana -type d -exec chmod 755 {} ';'

  if [ -x /bin/systemctl ] ; then
    echo "### NOT starting on installation, please execute the following statements to configure grafana to start automatically using systemd"
    echo " sudo /bin/systemctl daemon-reload"
    echo " sudo /bin/systemctl enable grafana-server.service"
    echo "### You can start grafana-server by executing"
    echo " sudo /bin/systemctl start grafana-server.service"
  elif [ -x /sbin/chkconfig ] ; then
    echo "### NOT starting grafana-server by default on bootup, please execute"
    echo " sudo /sbin/chkconfig --add grafana-server"
    echo "### In order to start grafana-server, execute"
    echo " sudo service grafana-server start"
  fi
elif [ $1 -ge 2 ] ; then
  if [ "$RESTART_ON_UPGRADE" == "true" ]; then
    stopGrafana
    startGrafana
  fi
fi