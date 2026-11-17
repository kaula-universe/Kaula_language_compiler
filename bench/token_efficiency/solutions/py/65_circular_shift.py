# BENCH:PREFIX

# BENCH:BEGIN
def circular_shift(x, shift):
    s = str(x)
    if shift > len(s):
        return s[::-1]
    return s[len(s) - shift:] + s[:len(s) - shift]

# BENCH:END

# BENCH:MAIN:BEGIN
if __name__ == "__main__":
    assert circular_shift(100, 2) == "001"
    assert circular_shift(12, 2) == "12"
    assert circular_shift(97, 8) == "79"
    assert circular_shift(11, 101) == "11"
    print("ok")
# BENCH:MAIN:END
