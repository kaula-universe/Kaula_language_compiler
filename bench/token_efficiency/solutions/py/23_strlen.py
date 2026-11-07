# BENCH:PREFIX

# BENCH:BEGIN
def strlen(string):
    return len(string)

# BENCH:END

# BENCH:MAIN:BEGIN
if __name__ == "__main__":
    assert strlen("") == 0
    assert strlen("abc") == 3
    print("ok")
# BENCH:MAIN:END
