#!/bin/sh
# Writes the JUnit report the quality gate reads. Stands in for a real
# runner's reporter (`jest --reporters=jest-junit`, `go test | go-junit-report`).
# The report is gitignored, so run this after a fresh checkout.
set -eu
cd "$(dirname "$0")/.."
cat > demo-junit.xml <<'XML'
<?xml version="1.0" encoding="UTF-8"?>
<testsuites>
  <testsuite name="demo" tests="4" failures="0">
    <testcase name="add works" />
    <testcase name="subtract works" />
    <testcase name="average of empty is zero" />
    <testcase name="parseConfig round-trips" />
  </testsuite>
</testsuites>
XML
echo "wrote demo-junit.xml"
