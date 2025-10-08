package com.example.pgtest.controller;

import com.example.pgtest.model.TestEntity;
import com.example.pgtest.service.ConnectionMonitor;
import com.example.pgtest.service.DatabaseService;
import lombok.extern.slf4j.Slf4j;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.*;

import java.time.LocalDateTime;
import java.util.HashMap;
import java.util.List;
import java.util.Map;

@RestController
@RequestMapping("/api")
@Slf4j
public class HealthController {

    @Autowired
    private DatabaseService databaseService;

    @Autowired
    private ConnectionMonitor connectionMonitor;

    @GetMapping("/health")
    public ResponseEntity<Map<String, Object>> health() {
        Map<String, Object> response = new HashMap<>();
        response.put("status", "UP");
        response.put("timestamp", LocalDateTime.now());
        response.put("application", "PostgreSQL Failover Test");

        try {
            boolean connectionValid = databaseService.testConnection();
            response.put("database", connectionValid ? "UP" : "DOWN");
            return ResponseEntity.ok(response);
        } catch (Exception e) {
            response.put("database", "DOWN");
            response.put("error", e.getMessage());
            return ResponseEntity.status(HttpStatus.SERVICE_UNAVAILABLE).body(response);
        }
    }

    @GetMapping("/db-info")
    public ResponseEntity<Map<String, Object>> getDatabaseInfo() {
        try {
            Map<String, Object> dbInfo = databaseService.getDatabaseInfo();
            return ResponseEntity.ok(dbInfo);
        } catch (Exception e) {
            log.error("Error getting database info", e);
            return ResponseEntity.status(HttpStatus.INTERNAL_SERVER_ERROR)
                    .body(Map.of("error", e.getMessage()));
        }
    }

    @GetMapping("/pool-info")
    public ResponseEntity<Map<String, Object>> getPoolInfo() {
        try {
            Map<String, Object> poolInfo = databaseService.getConnectionPoolInfo();
            return ResponseEntity.ok(poolInfo);
        } catch (Exception e) {
            log.error("Error getting pool info", e);
            return ResponseEntity.status(HttpStatus.INTERNAL_SERVER_ERROR)
                    .body(Map.of("error", e.getMessage()));
        }
    }

    @GetMapping("/monitor-stats")
    public ResponseEntity<Map<String, Object>> getMonitorStats() {
        try {
            Map<String, Object> stats = connectionMonitor.getMonitoringStats();
            return ResponseEntity.ok(stats);
        } catch (Exception e) {
            log.error("Error getting monitor stats", e);
            return ResponseEntity.status(HttpStatus.INTERNAL_SERVER_ERROR)
                    .body(Map.of("error", e.getMessage()));
        }
    }

    @PostMapping("/write-test")
    public ResponseEntity<Map<String, Object>> writeTest(@RequestParam(defaultValue = "Test message") String message) {
        try {
            TestEntity entity = databaseService.writeTestRecord(message);
            Map<String, Object> response = new HashMap<>();
            response.put("success", true);
            response.put("id", entity.getId());
            response.put("message", entity.getMessage());
            response.put("hostname", entity.getHostname());
            response.put("createdAt", entity.getCreatedAt());

            return ResponseEntity.ok(response);
        } catch (Exception e) {
            log.error("Error writing test record", e);
            return ResponseEntity.status(HttpStatus.INTERNAL_SERVER_ERROR)
                    .body(Map.of("success", false, "error", e.getMessage()));
        }
    }

    @GetMapping("/read-test")
    public ResponseEntity<Map<String, Object>> readTest() {
        try {
            List<TestEntity> records = databaseService.getAllTestRecords();
            Map<String, Object> response = new HashMap<>();
            response.put("success", true);
            response.put("count", records.size());
            response.put("records", records);

            return ResponseEntity.ok(response);
        } catch (Exception e) {
            log.error("Error reading test records", e);
            return ResponseEntity.status(HttpStatus.INTERNAL_SERVER_ERROR)
                    .body(Map.of("success", false, "error", e.getMessage()));
        }
    }

    @GetMapping("/test-connection")
    public ResponseEntity<Map<String, Object>> testConnection() {
        try {
            boolean valid = databaseService.testConnection();
            Map<String, Object> response = new HashMap<>();
            response.put("connectionValid", valid);
            response.put("timestamp", LocalDateTime.now());

            if (valid) {
                Map<String, Object> dbInfo = databaseService.getDatabaseInfo();
                response.putAll(dbInfo);
            }

            return ResponseEntity.ok(response);
        } catch (Exception e) {
            log.error("Connection test failed", e);
            return ResponseEntity.status(HttpStatus.INTERNAL_SERVER_ERROR)
                    .body(Map.of("connectionValid", false, "error", e.getMessage()));
        }
    }
}
