# Multi-stage Dockerfile for Spring Boot Application
# Supports both amd64 and arm64 architectures
# Uses BuildKit cache mounts for faster builds

# Stage 1: Build the application
FROM --platform=$BUILDPLATFORM maven:3.9-eclipse-temurin-17 AS builder

WORKDIR /app

# Copy pom.xml and download dependencies (cached layer)
COPY pom.xml .
RUN --mount=type=cache,target=/root/.m2 \
    mvn dependency:go-offline -B

# Copy source code and build
COPY src ./src
RUN --mount=type=cache,target=/root/.m2 \
    mvn package -DskipTests -B && \
    # Copy the built JAR to a location outside the cache mount
    mkdir -p /app/output && \
    cp /app/target/*.jar /app/output/app.jar

# Stage 2: Runtime image
FROM eclipse-temurin:17-jre-jammy

# Set up non-root user for security
RUN groupadd -r spring && useradd -r -g spring spring

WORKDIR /app

# Copy the built JAR from builder stage
COPY --from=builder /app/output/app.jar app.jar

# Change ownership to non-root user
RUN chown -R spring:spring /app

USER spring

# JVM options for container environments
ENV JAVA_TOOL_OPTIONS="-XX:+UseContainerSupport -XX:MaxRAMPercentage=75.0 -Djava.security.egd=file:/dev/./urandom"

# Expose application port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=40s --retries=3 \
  CMD curl -f http://localhost:8080/actuator/health || exit 1

# Run the application
ENTRYPOINT ["java", "-jar", "app.jar"]
