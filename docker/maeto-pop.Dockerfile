# PoP node: FRR for IS-IS + SRv6, strongSwan for customer IPsec termination.
FROM quay.io/frrouting/frr:10.4.1

RUN apk add --no-cache strongswan nftables

RUN : > /etc/swanctl/swanctl.conf

COPY docker/scripts/charon-maeto.conf /etc/strongswan.d/maeto.conf

COPY docker/scripts/pop-start.sh /usr/local/bin/pop-start
RUN chmod 0755 /usr/local/bin/pop-start

CMD ["/usr/local/bin/pop-start"]
