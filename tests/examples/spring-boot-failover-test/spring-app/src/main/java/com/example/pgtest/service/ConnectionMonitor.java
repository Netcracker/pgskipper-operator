package com.example.pgtest.service;

import lombok.extern.slf4j.Slf4j;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.scheduling.annotation.Scheduled;
import org.springframework.stereotype.Service;

import java.time.LocalDateTime;
import java.time.format.DateTimeFormatter;
import java.util.Map;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.atomic.AtomicLong;

@Service
@Slf4j
public class ConnectionMonitor {

    @Autowired
    private DatabaseService databaseService;

    private final AtomicInteger consecutiveFailures = new AtomicInteger(0);
    private final AtomicInteger consecutiveSuccesses = new AtomicInteger(0);
    private final AtomicLong totalChecks = new AtomicLong(0);
    private final AtomicLong totalFailures = new AtomicLong(0);
    private String lastKnownPrimary = "UNKNOWN";
    private String lastKnownRole = "UNKNOWN";

    private static final DateTimeFormatter FORMATTER = DateTimeFormatter.ofPattern("yyyy-MM-dd HH:mm:ss");

    /**
     * Monitor database connection every 5 seconds
     */
    @Scheduled(fixedRate = 5000, initialDelay = 10000)
    public void monitorConnection() {
        totalChecks.incrementAndGet();
        String timestamp = LocalDateTime.now().format(FORMATTER);

        try {
            Map<String, Object> dbInfo = databaseService.getDatabaseInfo();

            if ("CONNECTED".equals(dbInfo.get("status"))) {
                consecutiveSuccesses.incrementAndGet();
                int failures = consecutiveFailures.getAndSet(0);

                String currentRole = (String) dbInfo.get("role");
                String currentServer = (String) dbInfo.get("serverAddress");

                // Detect role change (failover)
                if (!lastKnownRole.equals("UNKNOWN") && !lastKnownRole.equals(currentRole)) {
                    log.warn("========================================");
                    log.warn("ROLE CHANGE DETECTED!");
                    log.warn("Previous role: {}", lastKnownRole);
                    log.warn("Current role: {}", currentRole);
                    log.warn("Previous server: {}", lastKnownPrimary);
                    log.warn("Current server: {}", currentServer);
                    log.warn("Consecutive failures before recovery: {}", failures);
                    log.warn("========================================");
                }

                // Detect server change (failover)
                if (!lastKnownPrimary.equals("UNKNOWN") &&
                    !lastKnownPrimary.equals(currentServer) &&
                    "PRIMARY".equals(currentRole)) {
                    log.warn("========================================");
                    log.warn("PRIMARY SERVER CHANGE DETECTED!");
                    log.warn("Previous primary: {}", lastKnownPrimary);
                    log.warn("New primary: {}", currentServer);
                    log.warn("Consecutive failures before recovery: {}", failures);
                    log.warn("========================================");
                }

                lastKnownRole = currentRole;
                if ("PRIMARY".equals(currentRole)) {
                    lastKnownPrimary = currentServer;
                }

                if (failures > 0) {
                    log.info("[{}] ✓ CONNECTION RESTORED - Server: {}, Role: {}, Downtime checks: {}",
                            timestamp, currentServer, currentRole, failures);
                } else if (consecutiveSuccesses.get() % 12 == 1) {  // Log every minute (12 * 5s)
                    log.info("[{}] ✓ Healthy - Server: {}, Role: {}, Total checks: {}, Total failures: {}",
                            timestamp, currentServer, currentRole, totalChecks.get(), totalFailures.get());
                }

            } else {
                handleConnectionFailure(timestamp, dbInfo);
            }

        } catch (Exception e) {
            handleConnectionFailure(timestamp, Map.of("error", e.getMessage()));
        }
    }

    private void handleConnectionFailure(String timestamp, Map<String, Object> dbInfo) {
        consecutiveFailures.incrementAndGet();
        totalFailures.incrementAndGet();
        consecutiveSuccesses.set(0);

        String error = dbInfo.get("error") != null ? dbInfo.get("error").toString() : "Unknown error";

        log.error("[{}] ✗ CONNECTION FAILED - Consecutive failures: {}, Total failures: {}, Error: {}",
                timestamp, consecutiveFailures.get(), totalFailures.get(), error);

        if (consecutiveFailures.get() == 1) {
            log.warn("========================================");
            log.warn("DATABASE CONNECTION LOST!");
            log.warn("Last known primary: {}", lastKnownPrimary);
            log.warn("Last known role: {}", lastKnownRole);
            log.warn("========================================");
        }
    }

    public Map<String, Object> getMonitoringStats() {
        return Map.of(
                "totalChecks", totalChecks.get(),
                "totalFailures", totalFailures.get(),
                "consecutiveFailures", consecutiveFailures.get(),
                "consecutiveSuccesses", consecutiveSuccesses.get(),
                "lastKnownPrimary", lastKnownPrimary,
                "lastKnownRole", lastKnownRole,
                "uptime", String.format("%.2f%%",
                        100.0 * (totalChecks.get() - totalFailures.get()) / Math.max(totalChecks.get(), 1))
        );
    }
}
