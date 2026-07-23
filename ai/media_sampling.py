def sample_ratios(duration):
    duration = max(float(duration or 0), 0.0)
    if duration < 10:
        return [0.25, 0.5, 0.75]
    if duration <= 60:
        return [0.1, 0.5, 0.9]
    return [0.1, 0.3, 0.5, 0.7, 0.9]
