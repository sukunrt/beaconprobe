import matplotlib.pyplot as plt
import numpy as np

data = np.loadtxt("attestations.ms")

fig, axes = plt.subplots(1, 2, figsize=(14, 5))

# Histogram
axes[0].hist(data, bins=50, edgecolor="black", alpha=0.7)
axes[0].set_xlabel("Time (ms)")
axes[0].set_ylabel("Count")
axes[0].set_title("Attestation Arrival Time Distribution")
axes[0].axvline(np.median(data), color="red", linestyle="--", label=f"Median: {np.median(data):.0f} ms")
axes[0].axvline(np.mean(data), color="orange", linestyle="--", label=f"Mean: {np.mean(data):.0f} ms")
axes[0].legend()

# CDF
sorted_data = np.sort(data)
cdf = np.arange(1, len(sorted_data) + 1) / len(sorted_data)
axes[1].plot(sorted_data, cdf)
axes[1].set_xlabel("Time (ms)")
axes[1].set_ylabel("Cumulative Fraction")
axes[1].set_title("CDF of Attestation Arrival Times")
axes[1].axhline(0.5, color="gray", linestyle=":", alpha=0.5)
axes[1].axhline(0.95, color="gray", linestyle=":", alpha=0.5, label="p50 / p95")
axes[1].legend()

fig.suptitle(f"Attestations (n={len(data)}, p50={np.percentile(data, 50):.0f} ms, p95={np.percentile(data, 95):.0f} ms, p99={np.percentile(data, 99):.0f} ms)")
plt.tight_layout()
plt.savefig("attestations.png", dpi=150)
print(f"Saved attestations.png")
print(f"  Min: {data.min():.0f} ms, Max: {data.max():.0f} ms")
print(f"  Mean: {data.mean():.0f} ms, Median: {np.median(data):.0f} ms")
print(f"  p95: {np.percentile(data, 95):.0f} ms, p99: {np.percentile(data, 99):.0f} ms")
