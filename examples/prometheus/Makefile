# Makefile for Prometheus rules rendering and checking

YTT ?= ytt
RULES_SRC := rules-src
GENERATED_RULES := perforce_rules.yml custom_perforce_rules.yml

.PHONY: render check-generated validate restart

render: $(GENERATED_RULES)

perforce_rules.yml: $(RULES_SRC)/perforce_rules.yml $(RULES_SRC)/default-values.yml
	$(YTT) -f $(RULES_SRC)/perforce_rules.yml -f $(RULES_SRC)/default-values.yml > $@

custom_perforce_rules.yml: $(RULES_SRC)/custom_perforce_rules.yml $(RULES_SRC)/default-values.yml $(RULES_SRC)/custom-values.yml
	$(YTT) -f $(RULES_SRC)/custom_perforce_rules.yml -f $(RULES_SRC)/default-values.yml -f $(RULES_SRC)/custom-values.yml > $@

check-generated: render
	git diff --exit-code -- $(GENERATED_RULES)

validate: render
	promtool check config prometheus.yml
	promtool check rules $(GENERATED_RULES)
	vmalert-prod -dryRun -rule perforce_rules.yml
	vmalert-prod -dryRun -rule custom_perforce_rules.yml
	vmauth-prod -dryRun -auth.config vmauth.yml
	# Use Cloudflare Prometheus linter https://github.com/cloudflare/pint
	# This version of pint is configured to use the same rules as Prometheus, but also checks for Victoria Metrics syntax.
	# https://github.com/rcowham/pint
	pint --config ./pint_vm.hcl --show-duplicates lint $(GENERATED_RULES)

restart: validate
	sudo systemctl restart prometheus
	sudo systemctl restart alertmanager
	sudo systemctl restart vmalert
	sudo systemctl restart vmauth