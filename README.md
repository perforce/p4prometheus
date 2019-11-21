# Docker for Helix Core
この環境は https://github.com/rcowham/p4prometheus から派生して作成しています。

環境を起動し終えると、次のサーバが稼働します。
- Helix Coreのコミットサーバ (SSLなし、Unicodeモード、サンプルDepot付き、p4prometheus入り)
- Helix Coreのエッジサーバ
- Prometheus
- Grafana

docker-composeを以下のように実行します。
```bash
docker-compose build
docker-compose up -d
```

実行後は、以下のイメージを元にしたコンテナが起動します。
ホスト名 | IMAGE名 | ポート設定1 | ポート設定2 
--- | --- | --- | ---
grafana | grafana/grafana | 3000:3000 |
monitor | p4prometheus_monitor | 9090:9090 | 9100:9100
master | p4prometheus_master | 2166:1999 | 9101:9100
replica_edge | p4prometheus_replica_edge | 2266:1999 | 9101:9100

互いのリンク状態は以下のとおりです。
grafana -> monitor
monitor -> master
replica_edge -> master 

コンテナを起動させるだけでは、Helix Coreのコミットサーバとエッジサーバが起動しません。

コンテナにログインをしてコミットサーバとエッジサーバの構築用シェルを実行します。
```bash
# 例
docker exec -it p4prometheus_master_1 /bin/bash
cd /p4
./configure_master.sh
```

実行後は master のコンテナ内でHelix Coreのコミットサーバ、replica_edge のコンテナ内でHelix Coreのエッジサーバが起動します。

Dockerのホスト側のIPアドレスが 192.168.1.2 であると仮定した場合、それぞれのツールに以下の方法でアクセスできます。

ツール | アクセスに使うツール | アクセス方法 | ユーザ | パスワード
--- | --- | --- 
grafana | WEBブラウザ | http://192.168.1.2:3000 | admin | admin
prometheus | WEBブラウザ | http://192.168.1.2:9090 | なし | なし
Helix Coreコミットサーバ | P4Vなど | 192.168.1.2:2166 | bruno | なし
Helix Coreエッジサーバ | P4Vなど | 192.168.1.2:2266 | bruno | なし
