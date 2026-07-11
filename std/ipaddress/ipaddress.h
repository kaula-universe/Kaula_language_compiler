#ifndef STD_IPADDRESS_IPADDRESS_H
#define STD_IPADDRESS_IPADDRESS_H

#include "../base/types.h"

typedef enum {
    IPV4,
    IPV6
} IPAddressFamily;

typedef struct IPAddress IPAddress;

IPAddress* ipaddress_create(const char* str);
IPAddress* ipaddress_create_from_bytes(const u8* bytes, IPAddressFamily family);
void ipaddress_destroy(IPAddress* ip);

IPAddressFamily ipaddress_family(const IPAddress* ip);
bool_t ipaddress_is_valid(const char* str);

char* ipaddress_to_string(const IPAddress* ip);
void ipaddress_to_bytes(const IPAddress* ip, u8* buffer);

bool_t ipaddress_equal(const IPAddress* a, const IPAddress* b);
bool_t ipaddress_is_loopback(const IPAddress* ip);
bool_t ipaddress_is_private(const IPAddress* ip);
bool_t ipaddress_is_multicast(const IPAddress* ip);
bool_t ipaddress_is_global(const IPAddress* ip);
bool_t ipaddress_is_link_local(const IPAddress* ip);
bool_t ipaddress_is_site_local(const IPAddress* ip);

bool_t ipaddress_in_network(const IPAddress* ip, const IPAddress* network, i32 prefix_len);

IPAddress* ipaddress_parse_cidr(const char* cidr, i32* prefix_len);

bool_t ipaddress_is_v4_mapped(const IPAddress* ip);
IPAddress* ipaddress_to_v4(const IPAddress* ip);

#endif
