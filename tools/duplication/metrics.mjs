import { createHash } from "node:crypto";

export const metricNames = ["clones", "duplicatedLines", "duplicatedTokens"];
export const policyHash = (policy) =>
  createHash("sha256").update(JSON.stringify(policy)).digest("hex");

export function metrics(report) {
  const result = Object.fromEntries(
    metricNames.map((name) => [name, report.statistics?.total?.[name]]),
  );
  if (
    Object.values(result).some(
      (value) => !Number.isSafeInteger(value) || value < 0,
    )
  ) {
    throw new Error("Scanner returned missing or invalid duplication metrics");
  }
  return result;
}

export function validateBaseline(baseline, policy) {
  if (baseline?.schemaVersion !== 1)
    throw new Error("Invalid duplication baseline schema");
  if (
    baseline.status === "pending-bootstrap" &&
    Object.keys(baseline).length === 2
  )
    return baseline;
  if (
    baseline.status !== "established" ||
    baseline.policyHash !== policyHash(policy) ||
    baseline.toolVersion !== policy.toolVersion ||
    !/^[a-f0-9]{40,64}$/.test(baseline.sourceCommit || "") ||
    Object.keys(baseline.scans || {})
      .sort()
      .join() !==
      policy.scans
        .map((scan) => scan.id)
        .sort()
        .join()
  ) {
    throw new Error(
      "Missing, corrupt or incompatible baseline; do not silently reinitialise it",
    );
  }
  for (const scan of Object.values(baseline.scans))
    metrics({ statistics: { total: scan } });
  return baseline;
}

export function limits(...ceilings) {
  return Object.fromEntries(
    metricNames.map((metric) => {
      const values = ceilings.filter(Boolean).map((ceiling) => ceiling[metric]);
      return [metric, values.length ? Math.min(...values) : null];
    }),
  );
}

export function regressions(actual, ...ceilings) {
  const bound = limits(...ceilings);
  return metricNames.flatMap((metric) => {
    const limit = bound[metric];
    return limit !== null && actual[metric] > limit
      ? [
          {
            metric,
            actual: actual[metric],
            limit,
            increase: actual[metric] - limit,
          },
        ]
      : [];
  });
}

export function ratchet(baseline, observed, sourceCommit, policy) {
  validateBaseline(baseline, policy);
  if (baseline.status === "established") {
    if (
      policy.scans.some(
        (scan) =>
          regressions(observed[scan.id], baseline.scans[scan.id]).length,
      )
    ) {
      throw new Error("Refusing to increase duplication limits");
    }
    if (
      policy.scans.every((scan) =>
        metricNames.every(
          (metric) =>
            observed[scan.id][metric] === baseline.scans[scan.id][metric],
        ),
      )
    )
      return null;
  }
  return {
    schemaVersion: 1,
    status: "established",
    toolVersion: policy.toolVersion,
    policyHash: policyHash(policy),
    sourceCommit,
    scans: observed,
  };
}

export function relativeClone(clone, root) {
  const prefix = root.replaceAll("\\", "/").replace(/\/$/, "") + "/";
  const file = (location) => {
    const path = location.name.replaceAll("\\", "/");
    if (
      !path.startsWith(prefix) ||
      path.slice(prefix.length).split("/").includes("..")
    )
      throw new Error("Scanner reported a path outside the repository");
    return { ...location, name: path.slice(prefix.length) };
  };
  return {
    ...clone,
    firstFile: file(clone.firstFile),
    secondFile: file(clone.secondFile),
  };
}

export const commandData = (value) =>
  String(value)
    .replaceAll("%", "%25")
    .replaceAll("\r", "%0D")
    .replaceAll("\n", "%0A");
export const commandProperty = (value) =>
  commandData(value).replaceAll(",", "%2C").replaceAll(":", "%3A");
