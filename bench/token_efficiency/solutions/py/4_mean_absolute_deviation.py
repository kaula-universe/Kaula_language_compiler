# BENCH:PREFIX

# BENCH:BEGIN
def mean_absolute_deviation(numbers):
    mean = sum(numbers) / len(numbers)
    return sum(abs(x - mean) for x in numbers) / len(numbers)

# BENCH:END

# BENCH:MAIN:BEGIN
if __name__ == "__main__":
    assert abs(mean_absolute_deviation([1.0, 2.0, 3.0]) - 2.0 / 3.0) < 1e-6
    assert abs(mean_absolute_deviation([1.0, 2.0, 3.0, 4.0]) - 1.0) < 1e-6
    print("ok")
# BENCH:MAIN:END
