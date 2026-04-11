export namespace main {
	
	export class IndexStatusDTO {
	    totalFiles: number;
	    indexedFiles: number;
	    failedFiles: number;
	    currentFile: string;
	    isRunning: boolean;
	    paused: boolean;
	    quotaPaused: boolean;
	    quotaResumeAt: string;
	
	    static createFrom(source: any = {}) {
	        return new IndexStatusDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.totalFiles = source["totalFiles"];
	        this.indexedFiles = source["indexedFiles"];
	        this.failedFiles = source["failedFiles"];
	        this.currentFile = source["currentFile"];
	        this.isRunning = source["isRunning"];
	        this.paused = source["paused"];
	        this.quotaPaused = source["quotaPaused"];
	        this.quotaResumeAt = source["quotaResumeAt"];
	    }
	}
	export class SearchResultDTO {
	    filePath: string;
	    fileName: string;
	    fileType: string;
	    extension: string;
	    sizeBytes: number;
	    thumbnailPath: string;
	    startTime: number;
	    endTime: number;
	    score: number;
	
	    static createFrom(source: any = {}) {
	        return new SearchResultDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.filePath = source["filePath"];
	        this.fileName = source["fileName"];
	        this.fileType = source["fileType"];
	        this.extension = source["extension"];
	        this.sizeBytes = source["sizeBytes"];
	        this.thumbnailPath = source["thumbnailPath"];
	        this.startTime = source["startTime"];
	        this.endTime = source["endTime"];
	        this.score = source["score"];
	    }
	}

}

