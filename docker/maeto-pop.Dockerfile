# PoP node: FRR for IS-IS + SRv6, strongSwan for customer IPsec termination.
FROM quay.io/frrouting/frr:10.4.1

RUN apk add --no-cache strongswan nftables supervisor

RUN mkdir -p /var/log/supervisor
COPY docker/conf/cpe/supervisord.conf /etc/supervisor/conf.d/supervisord.conf

RUN printf 'include conf.d/*.conf\n' > /etc/swanctl/swanctl.conf

COPY docker/conf/shared/charon-maeto.conf /etc/strongswan.d/maeto.conf

# WARN: frr daemon start '/usr/lib/frr/docker-start' happens in topology.yaml > exec:

CMD ["/usr/bin/supervisord", "-c", "/etc/supervisor/conf.d/supervisord.conf"]
