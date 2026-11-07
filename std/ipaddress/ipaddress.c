#include "ipaddress.h"
#include "../memory/memory.h"
#include "../string/string.h"
#include <stdlib.h>
#include <string.h>
#include <stdio.h>
#include <ctype.h>

struct IPAddress {
    IPAddressFamily family;
    u8 bytes[16];
};

static bool_t ipaddress_is_valid_v4(const char* str);
static bool_t ipaddress_is_valid_v6(const char* str);
static bool_t ipaddress_parse_v4(const char* str, u8* bytes);
static bool_t ipaddress_parse_v6(const char* str, u8* bytes);

IPAddress* ipaddress_create(const char* str) {
    if (!str) return NULL;
    
    IPAddress* ip = (IPAddress*)kmm_v4_malloc(sizeof(IPAddress));
    if (!ip) return NULL;
    
    if (ipaddress_is_valid_v4(str)) {
        ip->family = IPV4;
        ipaddress_parse_v4(str, ip->bytes);
    } else if (ipaddress_is_valid_v6(str)) {
        ip->family = IPV6;
        ipaddress_parse_v6(str, ip->bytes);
    } else {
        kmm_v4_free(ip);
        return NULL;
    }
    
    return ip;
}

IPAddress* ipaddress_create_from_bytes(const u8* bytes, IPAddressFamily family) {
    if (!bytes) return NULL;
    IPAddress* ip = (IPAddress*)kmm_v4_malloc(sizeof(IPAddress));
    if (!ip) return NULL;
    ip->family = family;
    memcpy(ip->bytes, bytes, family == IPV4 ? 4 : 16);
    if (family == IPV4) {
        memset(ip->bytes + 4, 0, 12);
    }
    return ip;
}

void ipaddress_destroy(IPAddress* ip) {
    if (ip) kmm_v4_free(ip);
}

IPAddressFamily ipaddress_family(const IPAddress* ip) {
    return ip ? ip->family : IPV4;
}

bool_t ipaddress_is_valid(const char* str) {
    return ipaddress_is_valid_v4(str) || ipaddress_is_valid_v6(str);
}

static bool_t ipaddress_is_valid_v4(const char* str) {
    if (!str) return false;
    int dots = 0;
    int digits = 0;
    while (*str) {
        if (isdigit(*str)) {
            digits++;
            if (digits > 3) return false;
        } else if (*str == '.') {
            if (digits == 0) return false;
            dots++;
            digits = 0;
        } else {
            return false;
        }
        str++;
    }
    return dots == 3 && digits > 0;
}

static bool_t ipaddress_is_valid_v6(const char* str) {
    if (!str) return false;
    int colons = 0;
    int groups = 0;
    bool_t double_colon = false;
    int hex_digits = 0;
    
    while (*str) {
        if (isxdigit(*str)) {
            hex_digits++;
            if (hex_digits > 4) return false;
        } else if (*str == ':') {
            if (hex_digits > 0) {
                groups++;
                hex_digits = 0;
            }
            if (*(str + 1) == ':') {
                if (double_colon) return false;
                double_colon = true;
                str++;
            }
            colons++;
        } else if (*str == '.') {
            return ipaddress_is_valid_v4(str);
        } else {
            return false;
        }
        str++;
    }
    
    if (hex_digits > 0) groups++;
    return (groups <= 8) && (double_colon || groups == 8);
}

static bool_t ipaddress_parse_v4(const char* str, u8* bytes) {
    memset(bytes, 0, 16);
    for (int i = 0; i < 4; i++) {
        int val = 0;
        while (*str && *str != '.') {
            val = val * 10 + (*str - '0');
            str++;
        }
        bytes[i] = (u8)val;
        if (*str == '.') str++;
    }
    return true;
}

static bool_t ipaddress_parse_v6(const char* str, u8* bytes) {
    memset(bytes, 0, 16);
    int idx = 0;
    bool_t double_colon = false;
    
    while (*str && idx < 16) {
        if (*str == ':') {
            if (*(str + 1) == ':') {
                double_colon = true;
                str += 2;
                int remaining_groups = 0;
                const char* temp = str;
                while (*temp) {
                    if (*temp == ':') remaining_groups++;
                    temp++;
                }
                if (!*str) remaining_groups++;
                idx = 16 - (remaining_groups * 2);
                continue;
            }
            str++;
        } else if (isdigit(*str) && (*(str + 1) == '.' || *(str + 2) == '.' || *(str + 3) == '.')) {
            ipaddress_parse_v4(str, bytes + idx);
            idx += 4;
            break;
        } else {
            int val = 0;
            while (isxdigit(*str)) {
                if (*str >= 'a' && *str <= 'f') val = val * 16 + (*str - 'a' + 10);
                else if (*str >= 'A' && *str <= 'F') val = val * 16 + (*str - 'A' + 10);
                else val = val * 16 + (*str - '0');
                str++;
            }
            bytes[idx++] = (u8)(val >> 8);
            bytes[idx++] = (u8)(val & 0xFF);
        }
    }
    
    if (double_colon && idx < 16) {
        memset(bytes + idx, 0, 16 - idx);
    }
    
    return true;
}

char* ipaddress_to_string(const IPAddress* ip) {
    if (!ip) return NULL;
    
    if (ip->family == IPV4) {
        char* str = (char*)kmm_v4_malloc(16);
        snprintf(str, 16, "%d.%d.%d.%d", ip->bytes[0], ip->bytes[1], ip->bytes[2], ip->bytes[3]);
        return str;
    } else {
        char* str = (char*)kmm_v4_malloc(40);
        bool_t compacted = false;
        int start_compact = -1;
        
        for (int i = 0; i < 8; i++) {
            u16 val = (ip->bytes[i * 2] << 8) | ip->bytes[i * 2 + 1];
            if (val == 0 && !compacted) {
                if (start_compact == -1) start_compact = i;
            } else {
                if (start_compact != -1) {
                    if (i == start_compact + 1) {
                        strcat(str, ":");
                    }
                    strcat(str, "::");
                    compacted = true;
                    start_compact = -1;
                }
                char group[5];
                snprintf(group, 5, "%x", val);
                strcat(str, group);
                if (i < 7) strcat(str, ":");
            }
        }
        
        if (start_compact != -1) {
            if (strlen(str) == 0) {
                strcpy(str, "::");
            } else {
                strcat(str, "::");
            }
        }
        
        return str;
    }
}

void ipaddress_to_bytes(const IPAddress* ip, u8* buffer) {
    if (!ip || !buffer) return;
    memcpy(buffer, ip->bytes, ip->family == IPV4 ? 4 : 16);
}

bool_t ipaddress_equal(const IPAddress* a, const IPAddress* b) {
    if (!a || !b) return false;
    return memcmp(a->bytes, b->bytes, 16) == 0;
}

bool_t ipaddress_is_loopback(const IPAddress* ip) {
    if (!ip) return false;
    if (ip->family == IPV4) {
        return ip->bytes[0] == 127;
    } else {
        return memcmp(ip->bytes, "\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x01", 16) == 0;
    }
}

bool_t ipaddress_is_private(const IPAddress* ip) {
    if (!ip) return false;
    if (ip->family == IPV4) {
        if (ip->bytes[0] == 10) return true;
        if (ip->bytes[0] == 172 && ip->bytes[1] >= 16 && ip->bytes[1] <= 31) return true;
        if (ip->bytes[0] == 192 && ip->bytes[1] == 168) return true;
    } else {
        if ((ip->bytes[0] & 0xFE) == 0xFC) return true;
        if (memcmp(ip->bytes, "\xFE\x80", 2) == 0) return true;
    }
    return false;
}

bool_t ipaddress_is_multicast(const IPAddress* ip) {
    if (!ip) return false;
    if (ip->family == IPV4) {
        return (ip->bytes[0] & 0xF0) == 0xE0;
    } else {
        return (ip->bytes[0] & 0xFF) == 0xFF;
    }
}

bool_t ipaddress_is_global(const IPAddress* ip) {
    if (!ip) return false;
    return !ipaddress_is_loopback(ip) && !ipaddress_is_private(ip) && !ipaddress_is_multicast(ip);
}

bool_t ipaddress_is_link_local(const IPAddress* ip) {
    if (!ip) return false;
    if (ip->family == IPV4) {
        return ip->bytes[0] == 169 && ip->bytes[1] == 254;
    } else {
        return memcmp(ip->bytes, "\xFE\x80", 2) == 0;
    }
}

bool_t ipaddress_is_site_local(const IPAddress* ip) {
    if (!ip) return false;
    if (ip->family == IPV4) {
        return ip->bytes[0] == 10 || (ip->bytes[0] == 172 && ip->bytes[1] >= 16 && ip->bytes[1] <= 31) ||
               (ip->bytes[0] == 192 && ip->bytes[1] == 168);
    } else {
        return memcmp(ip->bytes, "\xFD\x00", 2) == 0;
    }
}

bool_t ipaddress_in_network(const IPAddress* ip, const IPAddress* network, i32 prefix_len) {
    if (!ip || !network) return false;
    
    int full_bytes = prefix_len / 8;
    int remaining_bits = prefix_len % 8;
    
    if (memcmp(ip->bytes, network->bytes, (size_t)full_bytes) != 0) return false;
    
    if (remaining_bits > 0 && full_bytes < 16) {
        u8 mask = (u8)(0xFF << (8 - remaining_bits));
        if ((ip->bytes[full_bytes] & mask) != (network->bytes[full_bytes] & mask)) return false;
    }
    
    return true;
}

IPAddress* ipaddress_parse_cidr(const char* cidr, i32* prefix_len) {
    if (!cidr) return NULL;
    
    const char* slash = strchr(cidr, '/');
    if (!slash) return NULL;
    
    size_t ip_len = slash - cidr;
    char* ip_str = (char*)kmm_v4_malloc(ip_len + 1);
    strncpy(ip_str, cidr, ip_len);
    ip_str[ip_len] = '\0';
    
    *prefix_len = atoi(slash + 1);
    IPAddress* ip = ipaddress_create(ip_str);
    kmm_v4_free(ip_str);
    
    return ip;
}

bool_t ipaddress_is_v4_mapped(const IPAddress* ip) {
    if (!ip || ip->family != IPV6) return false;
    return memcmp(ip->bytes, "\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\xFF\xFF", 12) == 0;
}

IPAddress* ipaddress_to_v4(const IPAddress* ip) {
    if (!ip) return NULL;
    
    IPAddress* v4 = (IPAddress*)kmm_v4_malloc(sizeof(IPAddress));
    if (!v4) return NULL;
    
    v4->family = IPV4;
    if (ip->family == IPV4) {
        memcpy(v4->bytes, ip->bytes, 4);
    } else if (ipaddress_is_v4_mapped(ip)) {
        memcpy(v4->bytes, ip->bytes + 12, 4);
    } else {
        kmm_v4_free(v4);
        return NULL;
    }
    
    return v4;
}
