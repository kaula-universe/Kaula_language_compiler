# BENCH:BEGIN
def truncate_number(number):
    return number % 1.0

# BENCH:END

# BENCH:MAIN:BEGIN
if __name__ == "__main__":
    assert truncate_number(3.5) == 0.5
    assert abs(truncate_number(3.25) - 0.25) < 1e-9
    print("ok")
# BENCH:MAIN:END
