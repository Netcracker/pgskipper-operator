package com.example.pgtest.model;

import jakarta.persistence.*;
import lombok.Data;
import lombok.NoArgsConstructor;

import java.time.LocalDateTime;

@Entity
@Table(name = "connection_tests")
@Data
@NoArgsConstructor
public class TestEntity {

    @Id
    @GeneratedValue(strategy = GenerationType.IDENTITY)
    private Long id;

    @Column(nullable = false)
    private String message;

    @Column(name = "created_at", nullable = false)
    private LocalDateTime createdAt;

    @Column(name = "hostname")
    private String hostname;

    @PrePersist
    protected void onCreate() {
        createdAt = LocalDateTime.now();
    }

    public TestEntity(String message, String hostname) {
        this.message = message;
        this.hostname = hostname;
    }
}
