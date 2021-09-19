PHONY: check_python
check_python:
	@if ! python -c "import sys; sys.exit(0 if sys.version_info[0:2] == (3, 8) else 1)"; then \
		python --version; \
		echo -e "\nUse Python 3.8\n"; \
		false; \
	fi
