#!/usr/bin/env bb

(require '[clojure.tools.cli :as cli]
         '[clojure.string :as str]
         '[clojure.java.shell :as sh]
         '[clojure.walk :as walk]
         '[babashka.fs :as fs]
         '[cheshire.core :as cs])

(def cli-opts [["-p" "--projects PROJECTS" "Projects directory"
                :default "../projects"]
               ["-u" "--usages USAGES" "Labeled usages directory"
                :default "../labeled-usages-dataset"]
               ["-o" "--output OUT" "Output directory"
                :default "../labeled-cfg-dataset"]
               ["-b" "--binary BIN" "Geiger binary path"
                :default "./data-acquisition-tool/data-acquisition-tool"]])

(defn usage-files
  [projects-path]
  (fs/glob projects-path "**/*.txt"))

(defn get-output-path
  [{:keys [output usages]} usage-path]
  (let [dest-path (str/replace usage-path usages output)
        dest-path (str/replace dest-path ".txt" ".json")]
    dest-path))

(defn parse-usage
  [options file]
  (let [source (str file)
        dest (get-output-path options source)
        s (slurp source)
        [_ module version pkg file line project label1 label2]
        (re-find #"(?s)Module: ([^\n]+)\nVersion: ([^\n]+)\n\nPackage: ([^\n]+)\nFile: ([^\n]+)\nLine: ([^\n]+)\n\nImported[^:]*: ([^\n]+)\n\nLabel 1[^:]*: ([^\n]+)\nLabel 2[^:]*: ([^\n]+)" s)
        [_ snippet] (re-find #"(?s)Snippet line:\n+([^\n]+)\n" s)]
    (sorted-map
      :source source :dest dest
      :pkg pkg :file file :line line :snippet snippet
      :project project :label1 label1 :label2 label2
      :module module :version version)))

(defn cfg-created?
  [dest]
  (if (fs/exists? dest)
    (let [cfg (cs/parse-string (slurp dest) true)]
      (some? (seq (:cfg cfg))))
    false))

(defn get-cfg
  [{:keys [binary projects]} counter
   {:keys [pkg file line project dest source snippet] :as usage}]
  (when-not (cfg-created? dest)
    #_(locking *out* (println (swap! counter inc) "Skipping" source))
    (let [i (swap! counter inc)
          _ (locking *out* (println i "Creating CFG for" source))
          args [binary "cfg" "--base" projects "--project" project
                "--package" pkg "--file" file "--line" (str line) "--snippet" snippet]
          {:keys [out]}
          (apply sh/sh args)
          cfg (cs/parse-string out true)
          cfg (walk/prewalk (fn [node]
                              (if (map? node)
                                (dissoc node :position)
                                node))
                            cfg)]
       (when (empty? cfg)
         (locking *out* (println i "WARNING: Could not create CFG:" (str/join " " args))))
       {:usage usage :cfg (into (sorted-map) cfg)})))

(defn write-cfg
  [_ cfg]
  (when cfg
    (let [dest-path (-> cfg :usage :dest)
          cfg (update cfg :usage dissoc :source :dest)]
      (fs/create-dirs (fs/parent dest-path))
      (spit dest-path (cs/generate-string cfg {:pretty true})))))

(let [counter (atom 0)
      {:keys [options]} (cli/parse-opts *command-line-args* cli-opts)
      processor (comp (partial write-cfg options)
                      (partial get-cfg options counter)
                      (partial parse-usage options))
      files (usage-files (:usages options))]
  (println "Processing usages...")
  (doall (pmap processor files))
  (println "Done."))
