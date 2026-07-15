# Makefile for prometheus rules checking

validate:
	promtool check config prometheus.yml
	vmalert-prod -dryRun -rule perforce_rules.yml
	vmauth-prod -dryRun -auth.config vmauth.yml
	# Use Cloudflare Prometheus linter https://github.com/cloudflare/pint
	# This version of pint is configured to use the same rules as Prometheus, but also checks for Victoria Metrics syntax.
	# https://github.com/rcowham/pint
	pint --config ./pint_vm.hcl --show-duplicates lint perforce_rules.yml

restart: validate
	sudo systemctl restart prometheus
	sudo systemctl restart alertmanager
	sudo systemctl restart vmalert
	sudo systemctl restart vmauth