def sample_ratios(duration):
    duration = max(float(duration or 0), 0.0)
    if duration < 10:
        return [0.25, 0.5, 0.75]
    if duration <= 60:
        return [0.1, 0.5, 0.9]
    if duration <= 300:
        return [0.05, 0.2, 0.35, 0.5, 0.65, 0.8, 0.95]
    if duration <= 1200:
        return [0.05, 0.1625, 0.275, 0.3875, 0.5, 0.6125, 0.725, 0.8375, 0.95]
    return [0.05, 0.15, 0.25, 0.35, 0.45, 0.55, 0.65, 0.75, 0.85, 0.95]
