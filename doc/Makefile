# Makefile for SDP docs from AsciiDoctor format to HTML and PDF

SRC = $(wildcard *.adoc)

PDF = $(SRC:.adoc=.pdf)

HTML = $(SRC:.adoc=.html)

# Default target 'all' builds PDF and HTML from *.adoc files. For
# any referenced generated man pages, current files in the 'gen'
# directory are used.  See 'full'.
all:	$(PDF) $(HTML)

full:	$(PDF) $(HTML)

# .PHONY: Help

# General Rule for converting AsciiDoc files to HTML.
%.html: %.adoc
	rm -f $@
	asciidoctor -w -r asciidoctor-diagram $^

# General Rule for converting AsciiDoc files to PDF.
%.pdf: %.adoc
	rm -f $@
	asciidoctor-pdf -r asciidoctor-diagram -a pdf-themesdir=themes -a pdf-theme=basic $^

.PHONY: clean

clean:
	rm -f $(PDF) $(HTML)

validate:
	python3 check_html_links.py