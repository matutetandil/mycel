#!/bin/bash
# Test: SOAP endpoints
source "$(dirname "$0")/lib.sh"

echo "=== SOAP ==="

# WSDL endpoint
status=$(http_status GET "$SOAP/wsdl")
assert_status "GET /wsdl returns 200" "200" "$status"

body=$(http_body GET "$SOAP/wsdl")
assert_contains "WSDL contains definitions" "definitions|wsdl" "$body"

# SOAP CreateItem request
SOAP_CREATE='<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/" xmlns:ns="http://integration-test.mycel.dev/">
  <soap:Body>
    <ns:CreateItem>
      <title>Test Item</title>
      <status>active</status>
    </ns:CreateItem>
  </soap:Body>
</soap:Envelope>'

status=$(curl -so /dev/null -w "%{http_code}" -X POST -H "Content-Type: text/xml" -H "SOAPAction: CreateItem" -d "$SOAP_CREATE" "$SOAP/soap" 2>/dev/null)
assert_status "SOAP CreateItem returns 200" "200" "$status"

body=$(curl -sf -X POST -H "Content-Type: text/xml" -H "SOAPAction: CreateItem" -d "$SOAP_CREATE" "$SOAP/soap" 2>/dev/null)
assert_contains "SOAP response has Envelope" "Envelope" "$body"

# SOAP GetItem request
SOAP_GET='<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/" xmlns:ns="http://integration-test.mycel.dev/">
  <soap:Body>
    <ns:GetItem>
      <id>1</id>
    </ns:GetItem>
  </soap:Body>
</soap:Envelope>'

status=$(curl -so /dev/null -w "%{http_code}" -X POST -H "Content-Type: text/xml" -H "SOAPAction: GetItem" -d "$SOAP_GET" "$SOAP/soap" 2>/dev/null)
assert_status "SOAP GetItem returns 200" "200" "$status"

report
