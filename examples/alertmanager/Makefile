# Makefile to validate alertmanager config and restart service

validate:
	amtool check-config alertmanager.yml

restart: validate
	systemctl restart alertmanager

# Testing of Alertmanager templates - not always easy to get right!
test:
	amtool template render --template.glob='templates/*.tmpl' --template.text='{{ template "slack.default.text" . }}' --template.data=test.json
	amtool template render --template.glob='templates/*.tmpl' --template.text='{{ template "slack.default.text" . }}' --template.data=test1.json
